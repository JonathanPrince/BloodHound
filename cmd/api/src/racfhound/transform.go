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

// Package racfhound holds the fork's RACF-specific server code.
//
// transform.go is the client for the racfhound transform sidecar: a small
// Python service (`racfhound serve`) that turns an IRRDBU00 unload plus an
// optional runtime inventory into BloodHound OpenGraph JSON. The parsing lives
// in Python because that is where mfpandas and the RACF record definitions
// live; this file only moves bytes.
package racfhound

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// DefaultTransformServiceURL matches the racfhound-svc service name in
	// docker-compose.dev.yml.
	DefaultTransformServiceURL = "http://racfhound-svc:8000"

	// TransformServiceURLEnvVar overrides the sidecar location.
	TransformServiceURLEnvVar = "RACFHOUND_SERVICE_URL"

	// DefaultTransformTimeout bounds a single transform. A production-sized
	// IRRDBU00 unload is millions of records and parses for many minutes, so
	// this is generous by design.
	DefaultTransformTimeout = 60 * time.Minute

	// TransformTimeoutEnvVar overrides the timeout; any value time.ParseDuration
	// accepts ("90m", "2h").
	TransformTimeoutEnvVar = "RACFHOUND_SERVICE_TIMEOUT"
)

// DumpField and InventoryField are the two multipart fields the sidecar
// requires and treats specially. ListFields are the optional per-list DSN files
// and mirror LIST_INPUTS in racfhound/inventory.py — keep the two in step.
const (
	DumpField      = "dump"
	InventoryField = "inventory"
)

var ListFields = []string{"apf", "parmlib", "proclib", "lnklst", "lpa", "racfdb"}

// AcceptedFields is every multipart field the RACF ingest endpoint forwards.
// Anything else in the upload is dropped rather than passed along.
func AcceptedFields() []string {
	fields := make([]string, 0, len(ListFields)+2)
	fields = append(fields, DumpField, InventoryField)
	return append(fields, ListFields...)
}

// IsAcceptedField reports whether name is a field the sidecar understands.
func IsAcceptedField(name string) bool {
	if name == DumpField || name == InventoryField {
		return true
	}

	for _, field := range ListFields {
		if field == name {
			return true
		}
	}

	return false
}

// TransformInput is one file to forward, read from Path under the multipart
// field FieldName.
type TransformInput struct {
	FieldName string
	FileName  string
	Path      string
}

// TransformClient calls the racfhound transform sidecar.
type TransformClient struct {
	BaseURL string
	Client  *http.Client
}

// NewTransformClient builds a client from the environment, falling back to the
// compose defaults.
func NewTransformClient() TransformClient {
	baseURL := os.Getenv(TransformServiceURLEnvVar)
	if baseURL == "" {
		baseURL = DefaultTransformServiceURL
	}

	timeout := DefaultTransformTimeout
	if raw := os.Getenv(TransformTimeoutEnvVar); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			timeout = parsed
		}
	}

	return TransformClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client:  &http.Client{Timeout: timeout},
	}
}

// Transform posts the given files to the sidecar and returns the OpenGraph JSON
// response body. The body is streamed rather than buffered — an exported graph
// can run to hundreds of megabytes — so the caller must close it.
func (s TransformClient) Transform(ctx context.Context, inputs []TransformInput) (io.ReadCloser, error) {
	if len(inputs) == 0 {
		return nil, errors.New("no files to transform")
	}

	// Stream the multipart body out of the pipe as it is written so neither the
	// unload nor the graph is ever held in memory in full.
	bodyReader, bodyWriter := io.Pipe()
	form := multipart.NewWriter(bodyWriter)

	go func() {
		bodyWriter.CloseWithError(writeMultipart(form, inputs))
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+"/transform", bodyReader)
	if err != nil {
		bodyReader.CloseWithError(err)
		return nil, fmt.Errorf("building transform request: %w", err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())

	response, err := s.Client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("calling transform service at %s: %w", s.BaseURL, err)
	}

	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		return nil, fmt.Errorf("transform service returned %s: %s", response.Status, readErrorDetail(response.Body))
	}

	return response.Body, nil
}

// Healthy reports whether the sidecar answers its liveness probe. Used to fail
// an ingest fast with a clear message rather than after a long upload.
func (s TransformClient) Healthy(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+"/healthz", nil)
	if err != nil {
		return err
	}

	response, err := s.Client.Do(request)
	if err != nil {
		return fmt.Errorf("transform service at %s is unreachable: %w", s.BaseURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("transform service at %s returned %s", s.BaseURL, response.Status)
	}

	return nil
}

func writeMultipart(form *multipart.Writer, inputs []TransformInput) error {
	for _, input := range inputs {
		file, err := os.Open(input.Path)
		if err != nil {
			return fmt.Errorf("opening %s upload: %w", input.FieldName, err)
		}

		part, err := form.CreateFormFile(input.FieldName, input.FileName)
		if err != nil {
			file.Close()
			return fmt.Errorf("writing %s part: %w", input.FieldName, err)
		}

		_, err = io.Copy(part, file)
		file.Close()

		if err != nil {
			return fmt.Errorf("streaming %s upload: %w", input.FieldName, err)
		}
	}

	return form.Close()
}

// readErrorDetail pulls the FastAPI {"detail": ...} message out of an error
// response, capped so a stray HTML error page cannot flood the job status.
func readErrorDetail(body io.Reader) string {
	const maxDetail = 2048

	raw, err := io.ReadAll(io.LimitReader(body, maxDetail))
	if err != nil || len(raw) == 0 {
		return "no response body"
	}

	return strings.TrimSpace(string(raw))
}
