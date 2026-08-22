/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, RefreshCw, TestTube2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Main } from '@/components/layout'
import { PageFooterProvider } from '@/components/layout/components/page-footer'
import { Button } from '@/components/ui/button'
import { toast } from '@/hooks/use-toast'

import { fetchProxyNodes, testAllProxyNodes } from './proxy-node-api'
import { ProxyNodeView } from './proxy-node-view'

export function ProxyPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [footerContainer] = useState<HTMLDivElement | null>(null)

  const nodesQuery = useQuery({
    queryKey: ['proxy-nodes'],
    queryFn: fetchProxyNodes,
    retry: false,
  })

  const testAllMutation = useMutation({
    mutationFn: testAllProxyNodes,
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['proxy-nodes'] })
      toast.success(t('Tested {{passed}} of {{total}} nodes', result))
    },
    onError: (error: Error) => toast.error(error.message || t('Test failed')),
  })

  return (
    <PageFooterProvider container={footerContainer}>
      <Main>
        <div className='flex h-full min-h-0 flex-col'>
          <div className='flex shrink-0 flex-col gap-2 px-3 pt-3 pb-2.5 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between sm:gap-x-3 sm:gap-y-2 sm:px-4 sm:pt-5 sm:pb-3'>
            <div className='flex min-w-0 flex-wrap items-center gap-2 sm:gap-3'>
              <h2 className='truncate text-base font-bold tracking-tight sm:text-lg'>
                {t('Proxy Config')}
              </h2>
            </div>
            <div className='flex flex-wrap items-center gap-2'>
              <Button
                variant='outline'
                size='sm'
                onClick={() =>
                  queryClient.invalidateQueries({ queryKey: ['proxy-nodes'] })
                }
                disabled={nodesQuery.isFetching}
              >
                <RefreshCw className='size-4' />
                {t('Refresh')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                onClick={() => testAllMutation.mutate()}
                disabled={
                  (nodesQuery.data?.length ?? 0) === 0 ||
                  testAllMutation.isPending
                }
              >
                {testAllMutation.isPending ? (
                  <Loader2 className='size-4 animate-spin' />
                ) : (
                  <TestTube2 className='size-4' />
                )}
                {t('Test All Nodes')}
              </Button>
            </div>
          </div>
          <div className='min-h-0 flex-1 overflow-auto px-3 pt-1 pb-3 sm:px-4 sm:pt-1.5 sm:pb-4'>
            <ProxyNodeView />
          </div>
        </div>
      </Main>
    </PageFooterProvider>
  )
}
