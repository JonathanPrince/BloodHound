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

package racfhound_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/specterops/bloodhound/cmd/api/src/racfhound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTempFile drops content on disk and returns a TransformInput pointing at it.
func writeTempFile(t *testing.T, fieldName, fileName, content string) racfhound.TransformInput {
	t.Helper()

	path := filepath.Join(t.TempDir(), fileName)
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	return racfhound.TransformInput{FieldName: fieldName, FileName: fileName, Path: path}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) racfhound.TransformClient {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return racfhound.TransformClient{BaseURL: server.URL, Client: server.Client()}
}

func TestIsAcceptedField(t *testing.T) {
	t.Parallel()

	for _, field := range racfhound.AcceptedFields() {
		assert.True(t, racfhound.IsAcceptedField(field), "expected %q to be accepted", field)
	}

	assert.False(t, racfhound.IsAcceptedField("token"))
	assert.False(t, racfhound.IsAcceptedField(""))
	assert.False(t, racfhound.IsAcceptedField("APF"), "field matching is case sensitive")
}

func TestAcceptedFieldsCoversDumpInventoryAndEveryList(t *testing.T) {
	t.Parallel()

	// Mirrors LIST_INPUTS in racfhound/inventory.py; a new list must be added to
	// both, and this is the reminder.
	assert.Equal(t,
		[]string{"dump", "inventory", "apf", "parmlib", "proclib", "lnklst", "lpa", "racfdb"},
		racfhound.AcceptedFields(),
	)
}

func TestTransformForwardsEveryFileAsMultipart(t *testing.T) {
	t.Parallel()

	var received map[string]string

	client := newTestClient(t, func(response http.ResponseWriter, request *http.Request) {
		require.NoError(t, request.ParseMultipartForm(1<<20))

		received = map[string]string{}
		for field, headers := range request.MultipartForm.File {
			file, err := headers[0].Open()
			require.NoError(t, err)

			contents, err := io.ReadAll(file)
			require.NoError(t, err)
			file.Close()

			received[field] = string(contents)
		}

		response.Header().Set("Content-Type", "application/json")
		response.Write([]byte(`{"graph":{"nodes":[],"edges":[]}}`))
	})

	body, err := client.Transform(context.Background(), []racfhound.TransformInput{
		writeTempFile(t, "dump", "racfdump.txt", "0100 IBMUSER"),
		writeTempFile(t, "inventory", "inventory.yaml", "apf:\n  - SYS1.LINKLIB\n"),
		writeTempFile(t, "apf", "apflist.txt", "SYS1.SVCLIB\n"),
	})
	require.NoError(t, err)
	defer body.Close()

	graph, err := io.ReadAll(body)
	require.NoError(t, err)

	assert.JSONEq(t, `{"graph":{"nodes":[],"edges":[]}}`, string(graph))
	assert.Equal(t, map[string]string{
		"dump":      "0100 IBMUSER",
		"inventory": "apf:\n  - SYS1.LINKLIB\n",
		"apf":       "SYS1.SVCLIB\n",
	}, received)
}

func TestTransformRejectsEmptyInput(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(response http.ResponseWriter, request *http.Request) {
		t.Error("sidecar should not be called with no inputs")
	})

	_, err := client.Transform(context.Background(), nil)
	assert.ErrorContains(t, err, "no files to transform")
}

func TestTransformSurfacesServiceErrorDetail(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusBadRequest)
		response.Write([]byte(`{"detail":"Invalid inventory: section 'apf' must be a list of DSNs"}`))
	})

	_, err := client.Transform(context.Background(), []racfhound.TransformInput{
		writeTempFile(t, "dump", "racfdump.txt", "junk"),
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "400")
	assert.ErrorContains(t, err, "must be a list of DSNs")
}

func TestTransformFailsWhenAnInputIsMissing(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(response http.ResponseWriter, request *http.Request) {
		// Drain so the pipe write error surfaces on the client side.
		io.Copy(io.Discard, request.Body)
		response.Write([]byte(`{}`))
	})

	_, err := client.Transform(context.Background(), []racfhound.TransformInput{
		{FieldName: "dump", FileName: "racfdump.txt", Path: filepath.Join(t.TempDir(), "absent.txt")},
	})

	assert.Error(t, err)
}

func TestHealthy(t *testing.T) {
	t.Parallel()

	t.Run("ok", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t, func(response http.ResponseWriter, request *http.Request) {
			assert.Equal(t, "/healthz", request.URL.Path)
			response.Write([]byte(`{"status":"ok"}`))
		})

		assert.NoError(t, client.Healthy(context.Background()))
	})

	t.Run("non-200 is an error", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t, func(response http.ResponseWriter, request *http.Request) {
			response.WriteHeader(http.StatusInternalServerError)
		})

		assert.ErrorContains(t, client.Healthy(context.Background()), "500")
	})

	t.Run("unreachable is an error", func(t *testing.T) {
		t.Parallel()

		client := racfhound.TransformClient{BaseURL: "http://127.0.0.1:1", Client: http.DefaultClient}

		assert.ErrorContains(t, client.Healthy(context.Background()), "unreachable")
	})
}

func TestNewTransformClientReadsEnvironment(t *testing.T) {
	// t.Setenv forbids t.Parallel.
	t.Setenv(racfhound.TransformServiceURLEnvVar, "http://racf.example:9000/")
	t.Setenv(racfhound.TransformTimeoutEnvVar, "90m")

	client := racfhound.NewTransformClient()

	assert.Equal(t, "http://racf.example:9000", client.BaseURL, "trailing slash is trimmed")
	assert.Equal(t, 90*time.Minute, client.Client.Timeout)
}

func TestNewTransformClientDefaults(t *testing.T) {
	t.Setenv(racfhound.TransformServiceURLEnvVar, "")
	t.Setenv(racfhound.TransformTimeoutEnvVar, "not-a-duration")

	client := racfhound.NewTransformClient()

	assert.Equal(t, racfhound.DefaultTransformServiceURL, client.BaseURL)
	assert.Equal(t, racfhound.DefaultTransformTimeout, client.Client.Timeout, "an unparseable timeout falls back to the default")
}
