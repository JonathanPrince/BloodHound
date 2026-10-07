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

package v2_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/specterops/bloodhound/cmd/api/src/api/v2"
	"github.com/specterops/bloodhound/cmd/api/src/auth"
	"github.com/specterops/bloodhound/cmd/api/src/bhctx"
	"github.com/specterops/bloodhound/cmd/api/src/model"
	"github.com/specterops/bloodhound/cmd/api/src/racfhound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These cases cover only what RACFIngest does synchronously — auth, the sidecar
// pre-flight check, and upload validation. Everything past those points runs in
// a background goroutine against the shared ingest pipeline, which fileingest's
// own tests already cover.

// racfMultipartBody builds a multipart body from field name -> contents.
func racfMultipartBody(t *testing.T, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()

	var (
		body = &bytes.Buffer{}
		form = multipart.NewWriter(body)
	)

	for field, contents := range fields {
		part, err := form.CreateFormFile(field, field+".txt")
		require.NoError(t, err)

		_, err = part.Write([]byte(contents))
		require.NoError(t, err)
	}

	require.NoError(t, form.Close())

	return body, form.FormDataContentType()
}

// racfRequest builds an authenticated POST to the RACF ingest endpoint.
func racfRequest(t *testing.T, body *bytes.Buffer, contentType string, authenticated bool) *http.Request {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/api/v2/racf/ingest", body)
	request.Header.Set("Content-Type", contentType)

	requestCtx := bhctx.Context{RequestID: "id"}
	if authenticated {
		requestCtx.AuthCtx = auth.Context{Owner: model.User{}, Session: model.UserSession{}}
	}

	return request.WithContext(context.WithValue(context.Background(), bhctx.ValueKey, requestCtx.WithRequestID("id")))
}

// stubTransformService points the handler at a sidecar that reports healthy.
func stubTransformService(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv(racfhound.TransformServiceURLEnvVar, server.URL)
}

func TestResources_RACFIngest_Unauthorized(t *testing.T) {
	// t.Setenv in the helpers forbids t.Parallel throughout this file.
	body, contentType := racfMultipartBody(t, map[string]string{"dump": "0100 IBMUSER"})

	response := httptest.NewRecorder()
	v2.Resources{}.RACFIngest(response, racfRequest(t, body, contentType, false))

	assert.Equal(t, http.StatusUnauthorized, response.Code)
}

func TestResources_RACFIngest_ServiceUnavailable(t *testing.T) {
	// Port 1 refuses connections, standing in for a sidecar that is not running.
	t.Setenv(racfhound.TransformServiceURLEnvVar, "http://127.0.0.1:1")

	body, contentType := racfMultipartBody(t, map[string]string{"dump": "0100 IBMUSER"})

	response := httptest.NewRecorder()
	v2.Resources{}.RACFIngest(response, racfRequest(t, body, contentType, true))

	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Contains(t, response.Body.String(), "RACF transform service is unavailable")
}

func TestResources_RACFIngest_RejectsNonMultipart(t *testing.T) {
	stubTransformService(t)

	request := httptest.NewRequest(http.MethodPost, "/api/v2/racf/ingest", strings.NewReader(`{"dump":"nope"}`))
	request.Header.Set("Content-Type", "application/json")

	requestCtx := bhctx.Context{RequestID: "id", AuthCtx: auth.Context{Owner: model.User{}, Session: model.UserSession{}}}
	request = request.WithContext(context.WithValue(context.Background(), bhctx.ValueKey, requestCtx.WithRequestID("id")))

	response := httptest.NewRecorder()
	v2.Resources{}.RACFIngest(response, request)

	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Contains(t, response.Body.String(), "multipart")
}

func TestResources_RACFIngest_RequiresDump(t *testing.T) {
	stubTransformService(t)

	// An inventory on its own is not enough — the unload is the required input.
	body, contentType := racfMultipartBody(t, map[string]string{"inventory": "apf:\n  - SYS1.LINKLIB\n"})

	response := httptest.NewRecorder()
	v2.Resources{}.RACFIngest(response, racfRequest(t, body, contentType, true))

	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Contains(t, response.Body.String(), "dump")
}
