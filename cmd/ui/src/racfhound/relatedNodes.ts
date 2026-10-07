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

export type RACFRelatedNode = {
    objectID: string;
    name: string;
    label: string;
};

const normalizeRelatedNode = (node: Record<string, any>, fallbackKind: string): RACFRelatedNode => ({
    objectID: node.objectId || node.properties?.objectid || '',
    name: node.label || node.properties?.name || node.objectId || node.properties?.objectid || 'Unknown',
    label: node.kind || node.kinds?.[0] || fallbackKind,
});

export const fetchRACFRelatedNodes = async (
    databaseId: string,
    getQuery: (databaseId: string) => string,
    fallbackKind: string
) => {
    try {
        const response = await apiClient.cypherSearch(getQuery(databaseId), undefined, true);
        const nodes = response.data?.data?.nodes || {};

        // Sorted here rather than with ORDER BY in the Cypher: the pg graph
        // driver rejects `RETURN DISTINCT x ORDER BY x.name` (see the note in
        // groupMembers.ts). Sorting client-side works on either backend, and
        // these result sets are a panel's worth of rows, not a full graph.
        return Object.values(nodes)
            .map((node) => normalizeRelatedNode(node as Record<string, any>, fallbackKind))
            .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }));
    } catch (error) {
        const status = (error as { response?: { status?: number } }).response?.status;

        if (status === 404) {
            return [];
        }

        throw error;
    }
};
