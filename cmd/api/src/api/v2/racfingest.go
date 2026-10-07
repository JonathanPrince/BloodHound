// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

// RACF ingest — fork-specific. Accepts an IRRDBU00 unload plus an optional
// runtime inventory, has the racfhound sidecar turn them into OpenGraph JSON,
// and feeds the result through the ordinary ingest pipeline so the resulting
// graph and its job status behave exactly like any other file ingest.
//
// The transform is slow (minutes on a real RACF database), so the handler
// answers 202 as soon as the uploads are on local disk and does the rest in the
// background. Progress is the ingest job's own status, which the File Ingest
// table already renders.

package v2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/specterops/bloodhound/cmd/api/src/api"
	"github.com/specterops/bloodhound/cmd/api/src/auth"
	"github.com/specterops/bloodhound/cmd/api/src/bhctx"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/racfhound"
	"github.com/specterops/bloodhound/cmd/api/src/services/job"
	"github.com/specterops/bloodhound/cmd/api/src/services/upload"
	"github.com/specterops/bloodhound/packages/go/bhlog/attr"
	"github.com/specterops/bloodhound/packages/go/storage"
)

const (
	// racfIngestFileName is what the generated graph is called in the File
	// Ingest table, so a RACF ingest is recognisable among collector uploads.
	racfIngestFileName = "racf_opengraph.json"

	// racfHealthCheckTimeout bounds the pre-flight probe. Failing here saves the
	// user uploading a multi-gigabyte unload to a sidecar that is not running.
	racfHealthCheckTimeout = 5 * time.Second
)

// RACFIngest transforms an uploaded RACF unload into OpenGraph JSON and queues
// it for ingest. Responds 202 with the created ingest job.
func (s Resources) RACFIngest(response http.ResponseWriter, request *http.Request) {
	var (
		ctx       = request.Context()
		requestID = bhctx.FromRequest(request).RequestID
		client    = racfhound.NewTransformClient()
	)

	user, valid := auth.GetUserFromAuthCtx(bhctx.Get(ctx).AuthCtx)
	if !valid {
		api.WriteErrorResponse(ctx, api.BuildErrorResponse(http.StatusUnauthorized, api.ErrorResponseDetailsAuthenticationInvalid, request), response)
		return
	}

	healthCtx, cancelHealthCheck := context.WithTimeout(ctx, racfHealthCheckTimeout)
	defer cancelHealthCheck()

	if err := client.Healthy(healthCtx); err != nil {
		slog.ErrorContext(ctx, "RACF transform service health check failed", attr.Error(err))
		api.WriteErrorResponse(ctx, api.BuildErrorResponse(http.StatusServiceUnavailable, fmt.Sprintf("RACF transform service is unavailable: %v", err), request), response)
		return
	}

	inputs, workDir, err := saveRACFUploads(request)
	if err != nil {
		removeRACFWorkDir(ctx, workDir)
		api.WriteErrorResponse(ctx, api.BuildErrorResponse(http.StatusBadRequest, err.Error(), request), response)
		return
	}

	ingestJob, err := job.StartIngestJob(ctx, s.DB, user)
	if err != nil {
		removeRACFWorkDir(ctx, workDir)
		api.HandleDatabaseError(request, response, err)
		return
	}

	// The uploads now live in a directory we own rather than in the request's
	// multipart temp files, so the transform can safely outlive this handler.
	go func() {
		backgroundCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), racfhound.DefaultTransformTimeout)
		defer cancel()
		defer removeRACFWorkDir(backgroundCtx, workDir)

		if err := s.runRACFTransform(backgroundCtx, client, inputs, ingestJob, requestID); err != nil {
			slog.ErrorContext(backgroundCtx, "RACF ingest failed", slog.Int64("job_id", ingestJob.ID), attr.Error(err))
			s.failRACFIngestJob(backgroundCtx, ingestJob, err)
		}
	}()

	api.WriteBasicResponse(ctx, ingestJob, http.StatusAccepted, response)
}

// runRACFTransform calls the sidecar and hands the resulting graph to the
// standard ingest machinery.
func (s Resources) runRACFTransform(ctx context.Context, client racfhound.TransformClient, inputs []racfhound.TransformInput, ingestJob model.IngestJob, requestID string) error {
	graph, err := client.Transform(ctx, inputs)
	if err != nil {
		return err
	}
	defer graph.Close()

	ingestFileService, err := s.FileServiceResolver.Resolve(storage.FileServiceIngest)
	if err != nil {
		return fmt.Errorf("resolving ingest file service: %w", err)
	}

	// Same validation path a hand-uploaded OpenGraph file takes, so a malformed
	// graph fails here rather than inside the datapipe.
	validator := upload.NewIngestValidator(s.IngestSchema)

	storedFileName, err := upload.WriteAndValidateFile(
		ctx,
		ingestFileService,
		graph,
		fmt.Sprintf("file_upload_job%d_", ingestJob.ID),
		validator.WriteAndValidateJSON,
	)
	if err != nil {
		return fmt.Errorf("validating generated OpenGraph: %w", err)
	}

	if _, err := upload.CreateIngestTask(ctx, s.DB, upload.IngestTaskParams{
		Filename:         storedFileName,
		ProvidedFileName: racfIngestFileName,
		FileType:         model.FileTypeJson,
		RequestID:        requestID,
		JobID:            ingestJob.ID,
	}); err != nil {
		deleteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()

		if removeErr := ingestFileService.DeleteFile(deleteCtx, storedFileName); removeErr != nil {
			slog.WarnContext(ctx, "Failed to clean up RACF graph after task creation error", attr.Error(removeErr))
		}

		return fmt.Errorf("creating ingest task: %w", err)
	}

	if err := job.EndIngestJob(ctx, s.DB, ingestJob); err != nil {
		return fmt.Errorf("ending ingest job: %w", err)
	}

	slog.InfoContext(ctx, "RACF ingest queued for analysis", slog.Int64("job_id", ingestJob.ID))

	return nil
}

// failRACFIngestJob records the failure on the job so it surfaces in the File
// Ingest table instead of sitting at "Running" forever.
func (s Resources) failRACFIngestJob(ctx context.Context, ingestJob model.IngestJob, cause error) {
	ingestJob.Status = model.JobStatusFailed
	ingestJob.StatusMessage = cause.Error()
	ingestJob.EndTime = time.Now().UTC()

	if err := s.DB.UpdateIngestJob(ctx, ingestJob); err != nil {
		slog.ErrorContext(ctx, "Failed to mark RACF ingest job failed", slog.Int64("job_id", ingestJob.ID), attr.Error(err))
	}
}

// saveRACFUploads streams the multipart request into a temp directory owned by
// this process. net/http deletes its own multipart temp files once the handler
// returns, which the background transform would outlive.
func saveRACFUploads(request *http.Request) ([]racfhound.TransformInput, string, error) {
	reader, err := request.MultipartReader()
	if err != nil {
		return nil, "", fmt.Errorf("request must be a multipart upload: %w", err)
	}

	workDir, err := os.MkdirTemp("", "racf-ingest-")
	if err != nil {
		return nil, "", fmt.Errorf("creating working directory: %w", err)
	}

	var (
		inputs  []racfhound.TransformInput
		hasDump bool
	)

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, workDir, fmt.Errorf("reading upload: %w", err)
		}

		fieldName := part.FormName()
		if !racfhound.IsAcceptedField(fieldName) {
			part.Close()
			continue
		}

		fileName := part.FileName()
		if fileName == "" {
			fileName = fieldName
		}

		destination := filepath.Join(workDir, fieldName)

		file, err := os.Create(destination)
		if err != nil {
			part.Close()
			return nil, workDir, fmt.Errorf("creating temp file for %s: %w", fieldName, err)
		}

		_, copyErr := io.Copy(file, part)
		closeErr := file.Close()
		part.Close()

		if copyErr != nil {
			return nil, workDir, fmt.Errorf("saving %s upload: %w", fieldName, copyErr)
		} else if closeErr != nil {
			return nil, workDir, fmt.Errorf("saving %s upload: %w", fieldName, closeErr)
		}

		if fieldName == racfhound.DumpField {
			hasDump = true
		}

		inputs = append(inputs, racfhound.TransformInput{
			FieldName: fieldName,
			FileName:  fileName,
			Path:      destination,
		})
	}

	if !hasDump {
		return nil, workDir, fmt.Errorf("missing required field %q (the IRRDBU00 unload)", racfhound.DumpField)
	}

	return inputs, workDir, nil
}

func removeRACFWorkDir(ctx context.Context, workDir string) {
	if workDir == "" {
		return
	}

	if err := os.RemoveAll(workDir); err != nil {
		slog.WarnContext(ctx, "Failed to clean up RACF ingest working directory", slog.String("dir", workDir), attr.Error(err))
	}
}
