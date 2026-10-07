// Copyright 2023 Specter Ops, Inc.
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

import { Container } from '@mui/material';
import { useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import { addSnackbar } from 'src/ducks/global/actions';
import { useAppDispatch } from 'src/store';

interface LoginPageProps {
    children: React.ReactNode;
}

const LoginPage: React.FC<LoginPageProps> = ({ children }) => {
    const dispatch = useAppDispatch();
    const [searchParams] = useSearchParams();

    const errorMessage = searchParams.get('error');

    useEffect(() => {
        if (errorMessage) {
            dispatch(addSnackbar(errorMessage, 'SSOError', { variant: 'error' }));
        }
    }, [dispatch, errorMessage]);

    return (
        <div className='flex justify-center items-center h-full'>
            <Container maxWidth='sm'>
                <div className='bg-[#faf6ed] shadow-outer-1 px-16 pb-16 pt-8'>
                    {/*
                      RACFHound fork: the login mark. racfhound-logo.png has no
                      alpha channel — it carries a solid #faf6ed background — so the
                      login card uses that same colour and the logo blends in.
                    */}
                    <div className='h-full w-auto text-center box-border px-8 py-12'>
                        <span className='block overflow-hidden'>
                            <img
                                src={`${import.meta.env.BASE_URL}/img/racfhound-logo.png`}
                                alt='RACFHound'
                                className='block w-full'
                            />
                        </span>
                    </div>
                    {children}
                </div>
            </Container>
        </div>
    );
};

export default LoginPage;
