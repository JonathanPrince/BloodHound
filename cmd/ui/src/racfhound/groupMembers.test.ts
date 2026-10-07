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
import {
    getRACFClassUsersWithCLAUTHQuery,
    getRACFGroupCanSubmitAsQuery,
    getRACFGroupMembersQuery,
    getRACFGroupSubgroupsQuery,
    getRACFUserCanSubmitAsQuery,
    getRACFUserClassAuthoritiesQuery,
    getRACFUserGroupsQuery,
    getRACFUserSubmittedAsByQuery,
} from './groupMembers';
import { fetchRACFRelatedNodes } from './relatedNodes';

const allQueryBuilders = {
    getRACFGroupMembersQuery,
    getRACFGroupSubgroupsQuery,
    getRACFGroupCanSubmitAsQuery,
    getRACFUserGroupsQuery,
    getRACFUserCanSubmitAsQuery,
    getRACFUserSubmittedAsByQuery,
    getRACFUserClassAuthoritiesQuery,
    getRACFClassUsersWithCLAUTHQuery,
};

describe('RACF relationship queries', () => {
    // BloodHound's pg graph driver translates `RETURN DISTINCT x ORDER BY
    // x.name` into SQL Postgres rejects (SQLSTATE 42P10), which 500s every RACF
    // relationship panel. Neo4j accepts it, so this only shows up on a
    // pg-backed instance — hence the guard. Sorting lives in
    // fetchRACFRelatedNodes instead.
    it.each(Object.entries(allQueryBuilders))('%s does not sort in Cypher', (_name, build) => {
        expect(build('42')).not.toContain('ORDER BY');
    });

    it.each(Object.entries(allQueryBuilders))('%s returns a deduplicated result', (_name, build) => {
        expect(build('42')).toContain('RETURN DISTINCT');
    });

    it.each(Object.entries(allQueryBuilders))('%s rejects a non-integer database id', (_name, build) => {
        expect(() => build('7 OR 1=1')).toThrow();
        expect(() => build('')).toThrow();
    });
});

describe('fetchRACFRelatedNodes', () => {
    afterEach(() => {
        vi.restoreAllMocks();
    });

    const respondWith = (nodes: Record<string, any>) =>
        vi.spyOn(apiClient, 'cypherSearch').mockResolvedValue({ data: { data: { nodes } } } as any);

    it('sorts results by name, since the Cypher no longer does', async () => {
        respondWith({
            '1': { objectId: 'RACFUSER_ZULU', label: 'ZULU', kind: 'RACFUser' },
            '2': { objectId: 'RACFUSER_ALPHA', label: 'ALPHA', kind: 'RACFUser' },
            '3': { objectId: 'RACFUSER_mike', label: 'mike', kind: 'RACFUser' },
        });

        const result = await fetchRACFRelatedNodes('42', getRACFGroupMembersQuery, 'RACFUser');

        expect(result.map((node) => node.name)).toEqual(['ALPHA', 'mike', 'ZULU']);
    });

    it('treats a no-results 404 as an empty section rather than an error', async () => {
        vi.spyOn(apiClient, 'cypherSearch').mockRejectedValue({ response: { status: 404 } });

        await expect(fetchRACFRelatedNodes('42', getRACFGroupMembersQuery, 'RACFUser')).resolves.toEqual([]);
    });

    it('propagates other failures', async () => {
        vi.spyOn(apiClient, 'cypherSearch').mockRejectedValue({ response: { status: 500 } });

        await expect(fetchRACFRelatedNodes('42', getRACFGroupMembersQuery, 'RACFUser')).rejects.toBeDefined();
    });
});
