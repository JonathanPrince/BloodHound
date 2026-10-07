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

import userEvent from '@testing-library/user-event';
import { apiClient } from 'bh-shared-ui';
import { rest } from 'msw';
import { setupServer } from 'msw/node';
import { render, screen, waitFor } from 'src/test-utils';
import RACFIngest from './RACFIngest';
import { RACF_INGEST_ENDPOINT } from './racfIngestApi';

// usePermissions reads /api/v2/self, so granting IngestManage over msw exercises
// the real permission path rather than mocking the hook out.
const selfWithIngestManage = {
    data: {
        roles: [{ permissions: [{ authority: 'graphdb', name: 'IngestManage' }] }],
    },
};

const server = setupServer(
    rest.get('/api/v2/self', (_request, response, context) => response(context.json(selfWithIngestManage)))
);

// The upload itself is spied at the axios client rather than served by msw:
// jsdom's XHR does not carry a multipart FormData body through the msw
// interceptor, and the spy lets the FormData fields be asserted directly.
const postSpy = vi.spyOn(apiClient.baseClient, 'post');

const acceptedResponse = { data: { data: { id: 7, status: 1, status_message: '' } } };

/** The multipart field names of the most recent upload, in order. */
const postedFields = (): string[] => {
    const body = postSpy.mock.calls.at(-1)?.[1] as FormData;

    return [...body.keys()];
};

/** The File posted under a given field. */
const postedFile = (field: string): File => {
    const body = postSpy.mock.calls.at(-1)?.[1] as FormData;

    return body.get(field) as File;
};

beforeAll(() => server.listen());
beforeEach(() => {
    postSpy.mockReset();
    postSpy.mockResolvedValue(acceptedResponse as any);
});
afterEach(() => server.resetHandlers());
afterAll(() => {
    postSpy.mockRestore();
    server.close();
});

const dumpFile = () => new File(['0100 IBMUSER'], 'racfdump.txt', { type: 'text/plain' });
const inventoryFile = () => new File(['apf:\n  - SYS1.LINKLIB\n'], 'inventory.yaml', { type: 'text/yaml' });

/** Renders and waits for the permission lookup to land, which ungates the inputs. */
const renderReady = async () => {
    render(<RACFIngest />);

    await waitFor(() => expect(screen.getByTestId('racf-ingest_input-dump')).toBeEnabled());
};

describe('RACFIngest', () => {
    it('renders both inputs and disables submit until an unload is chosen', async () => {
        await renderReady();

        expect(screen.getByTestId('racf-ingest_input-inventory')).toBeInTheDocument();
        expect(screen.getByTestId('racf-ingest_button-submit')).toBeDisabled();
        expect(screen.queryByTestId('racf-ingest_alert-permission')).not.toBeInTheDocument();
    });

    it('enables submit once an unload is selected', async () => {
        const user = userEvent.setup();
        await renderReady();

        await user.upload(screen.getByTestId('racf-ingest_input-dump'), dumpFile());

        expect(screen.getByTestId('racf-ingest_selected-dump')).toHaveTextContent('racfdump.txt');
        expect(screen.getByTestId('racf-ingest_button-submit')).toBeEnabled();
    });

    it('posts the unload alone when no inventory is given', async () => {
        const user = userEvent.setup();
        await renderReady();

        await user.upload(screen.getByTestId('racf-ingest_input-dump'), dumpFile());
        await user.click(screen.getByTestId('racf-ingest_button-submit'));

        await waitFor(() => expect(screen.getByTestId('racf-ingest_alert-success')).toBeInTheDocument());

        expect(postSpy).toHaveBeenCalledWith(RACF_INGEST_ENDPOINT, expect.any(FormData));
        expect(postedFields()).toEqual(['dump']);
        expect(postedFile('dump').name).toBe('racfdump.txt');
    });

    it('posts the unload and inventory together and reports the job id', async () => {
        const user = userEvent.setup();
        await renderReady();

        await user.upload(screen.getByTestId('racf-ingest_input-dump'), dumpFile());
        await user.upload(screen.getByTestId('racf-ingest_input-inventory'), inventoryFile());
        await user.click(screen.getByTestId('racf-ingest_button-submit'));

        await waitFor(() => expect(screen.getByTestId('racf-ingest_alert-success')).toBeInTheDocument());

        expect(postedFields()).toEqual(['dump', 'inventory']);
        expect(postedFile('inventory').name).toBe('inventory.yaml');
        expect(screen.getByTestId('racf-ingest_alert-success')).toHaveTextContent('ingest job 7');
    });

    it('surfaces the API error message', async () => {
        postSpy.mockRejectedValue({
            response: { status: 503, data: { errors: [{ message: 'RACF transform service is unavailable' }] } },
        });

        const user = userEvent.setup();
        await renderReady();

        await user.upload(screen.getByTestId('racf-ingest_input-dump'), dumpFile());
        await user.click(screen.getByTestId('racf-ingest_button-submit'));

        await waitFor(() => expect(screen.getByTestId('racf-ingest_alert-error')).toBeInTheDocument());
        expect(screen.getByTestId('racf-ingest_alert-error')).toHaveTextContent(
            'RACF transform service is unavailable'
        );
    });

    it('clears the selection and the result', async () => {
        const user = userEvent.setup();
        await renderReady();

        await user.upload(screen.getByTestId('racf-ingest_input-dump'), dumpFile());
        await user.click(screen.getByTestId('racf-ingest_button-submit'));

        await waitFor(() => expect(screen.getByTestId('racf-ingest_alert-success')).toBeInTheDocument());

        await user.click(screen.getByTestId('racf-ingest_button-reset'));

        expect(screen.queryByTestId('racf-ingest_alert-success')).not.toBeInTheDocument();
        expect(screen.queryByTestId('racf-ingest_selected-dump')).not.toBeInTheDocument();
        expect(screen.getByTestId('racf-ingest_button-submit')).toBeDisabled();
    });

    it('blocks ingest without the IngestManage permission', async () => {
        server.use(rest.get('/api/v2/self', (_request, response, context) => response(context.json({ data: {} }))));

        render(<RACFIngest />);

        await waitFor(() => expect(screen.getByTestId('racf-ingest_alert-permission')).toBeInTheDocument());
        expect(screen.getByTestId('racf-ingest_input-dump')).toBeDisabled();
        expect(screen.getByTestId('racf-ingest_button-submit')).toBeDisabled();
        expect(postSpy).not.toHaveBeenCalled();
    });
});
