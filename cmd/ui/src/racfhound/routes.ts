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

// RACF route paths live here rather than in src/routes/constants.ts so the
// dependency runs one way: constants.ts imports this, never the reverse. The
// RACF Ingest page is lazy-imported by constants.ts, so a page that imported a
// constant back out of it would form a cycle and resolve to undefined at
// render time.

export const ROUTE_ADMINISTRATION_RACF_INGEST = '/administration/racf-ingest';
