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

import { apiClient } from 'bh-shared-ui';
import { useMutation, useQueryClient } from 'react-query';

export const RACF_INGEST_ENDPOINT = '/api/v2/racf/ingest';

export type RACFIngestUpload = {
    /** The IRRDBU00 unload produced by `racfhound collect`. Required. */
    dump: File;
    /** YAML runtime inventory (apf, parmlib, proclib, lnklst, lpa, racf_db). Optional. */
    inventory?: File | null;
};

export type RACFIngestJob = {
    id: number;
    status: number;
    status_message: string;
};

/**
 * Posts the unload and inventory to the API, which hands them to the racfhound
 * transform sidecar and queues the resulting OpenGraph for ingest.
 *
 * The response is a 202 with the created ingest job: the transform runs in the
 * background and its progress shows up in the File Ingest table.
 */
export const uploadRACFIngest = async ({ dump, inventory }: RACFIngestUpload): Promise<RACFIngestJob> => {
    const body = new FormData();
    body.append('dump', dump, dump.name);

    if (inventory) {
        body.append('inventory', inventory, inventory.name);
    }

    // Content-Type is deliberately unset — the browser has to add the multipart
    // boundary itself.
    const response = await apiClient.baseClient.post(RACF_INGEST_ENDPOINT, body);

    return response.data?.data;
};

/**
 * Extracts the API's error message, falling back to something actionable when
 * the failure has no structured body (a proxy timeout, say).
 */
export const racfIngestErrorMessage = (error: any): string =>
    error?.response?.data?.errors?.[0]?.message ||
    error?.message ||
    'Unable to start the RACF ingest. Check the API logs for details.';

export const useRACFIngest = () => {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: uploadRACFIngest,
        // The new job belongs in the File Ingest table straight away.
        onSettled: () => queryClient.invalidateQueries('file-upload'),
    });
};
