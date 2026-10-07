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

import { AppLink, PageWithTitle, Permission, usePermissions } from 'bh-shared-ui';
import { Alert, Button, Typography } from 'doodle-ui';
import { FC, FormEvent, useState } from 'react';
// Named racfIngestApi, not racfIngest: a module differing from RACFIngest.tsx
// only by case resolves to the wrong file on case-insensitive filesystems.
import { racfIngestErrorMessage, useRACFIngest } from './racfIngestApi';

// Linked by path, the way FileUploadDialog links the same page. Importing the
// constant from src/routes/constants would be a cycle — that module
// lazy-imports this page, and the cycle resolves this component to undefined.
const FILE_INGEST_PATH = '/administration/file-ingest';

type FilePickerProps = {
    id: string;
    label: string;
    description: string;
    accept?: string;
    required?: boolean;
    disabled?: boolean;
    file: File | null;
    onSelect: (file: File | null) => void;
};

const FilePicker: FC<FilePickerProps> = ({ id, label, description, accept, required, disabled, file, onSelect }) => (
    <div className='mb-6'>
        <label htmlFor={id} className='block mb-1 font-medium'>
            {label}
            {required ? (
                <span className='ml-1 text-error'>*</span>
            ) : (
                <span className='ml-1 opacity-60'>(optional)</span>
            )}
        </label>
        <Typography variant='body2' className='mb-2 opacity-80'>
            {description}
        </Typography>
        <input
            id={id}
            data-testid={`racf-ingest_input-${id}`}
            type='file'
            accept={accept}
            disabled={disabled}
            onChange={(event) => onSelect(event.target.files?.[0] ?? null)}
            className='block w-full max-w-xl text-sm file:mr-4 file:rounded file:border-0 file:bg-secondary file:px-4 file:py-2 file:text-secondary-foreground disabled:opacity-50'
        />
        {file && (
            <Typography variant='body2' className='mt-1 opacity-80' data-testid={`racf-ingest_selected-${id}`}>
                Selected: {file.name}
            </Typography>
        )}
    </div>
);

/**
 * RACF Ingest — fork-specific administration page.
 *
 * Takes an IRRDBU00 unload and an optional runtime inventory, hands them to the
 * API, and reports the ingest job it queues. The transform itself runs in the
 * racfhound sidecar and can take several minutes, so the job's progress is
 * followed on the File Ingest page rather than here.
 */
const RACFIngest: FC = () => {
    const { checkPermission } = usePermissions();
    const canIngest = checkPermission(Permission.GRAPH_DB_INGEST_MANAGE);

    const [dump, setDump] = useState<File | null>(null);
    const [inventory, setInventory] = useState<File | null>(null);

    const ingest = useRACFIngest();

    const handleSubmit = (event: FormEvent) => {
        event.preventDefault();

        if (!dump) {
            return;
        }

        ingest.mutate({ dump, inventory });
    };

    const handleReset = () => {
        setDump(null);
        setInventory(null);
        ingest.reset();
    };

    return (
        <PageWithTitle
            title='RACF Ingest'
            data-testid='racf-ingest'
            pageDescription={
                <Typography variant='body2'>
                    Upload an IRRDBU00 unload of the RACF database, optionally with a runtime inventory describing the
                    APF, PARMLIB, PROCLIB, LNKLST, LPA and RACF database datasets. RACFHound transforms them into an
                    OpenGraph and queues it for ingest.
                </Typography>
            }>
            {!canIngest && (
                <Alert variant='warning' className='mb-6' data-testid='racf-ingest_alert-permission'>
                    Your user role does not grant permission to upload data. Please contact your administrator for
                    details.
                </Alert>
            )}

            <form onSubmit={handleSubmit} data-testid='racf-ingest_form'>
                <FilePicker
                    id='dump'
                    label='IRRDBU00 unload'
                    description='The sequential dataset IRRDBU00 writes — racfdump.txt from `racfhound collect`, or any existing unload.'
                    required
                    disabled={!canIngest || ingest.isLoading}
                    file={dump}
                    onSelect={setDump}
                />

                <FilePicker
                    id='inventory'
                    label='Runtime inventory'
                    description='YAML listing the datasets that are not in the RACF database — apf, parmlib, proclib, lnklst, lpa, racf_db. Datasets named here are flagged on their node and inherit access from the generic profile that controls them.'
                    accept='.yaml,.yml,text/yaml,application/x-yaml'
                    disabled={!canIngest || ingest.isLoading}
                    file={inventory}
                    onSelect={setInventory}
                />

                <div className='flex items-center gap-4'>
                    <Button
                        type='submit'
                        data-testid='racf-ingest_button-submit'
                        disabled={!canIngest || !dump || ingest.isLoading}>
                        {ingest.isLoading ? 'Uploading…' : 'Process and Ingest'}
                    </Button>

                    {(dump || inventory || ingest.isSuccess || ingest.isError) && (
                        <Button
                            type='button'
                            variant='secondary'
                            onClick={handleReset}
                            data-testid='racf-ingest_button-reset'
                            disabled={ingest.isLoading}>
                            Clear
                        </Button>
                    )}
                </div>
            </form>

            {ingest.isSuccess && (
                <Alert variant='success' className='mt-6' data-testid='racf-ingest_alert-success'>
                    Upload accepted{ingest.data?.id ? ` as ingest job ${ingest.data.id}` : ''}. Transforming the unload
                    takes a few minutes on a large RACF database — follow its progress on the{' '}
                    <AppLink to={FILE_INGEST_PATH} className='underline'>
                        File Ingest
                    </AppLink>{' '}
                    page.
                </Alert>
            )}

            {ingest.isError && (
                <Alert variant='error' className='mt-6' data-testid='racf-ingest_alert-error'>
                    {racfIngestErrorMessage(ingest.error)}
                </Alert>
            )}
        </PageWithTitle>
    );
};

export default RACFIngest;
