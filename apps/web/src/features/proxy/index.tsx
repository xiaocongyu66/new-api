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
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { toast } from '@/hooks/use-toast'

import { fetchProxyNodes, testAllProxyNodes } from './proxy-node-api'
import { ProxyNodeView } from './proxy-node-view'

export function ProxyPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

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
    <SectionPageLayout>
      <SectionPageLayout.Title>
        <span className='truncate'>{t('Proxy Config')}</span>
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
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
            (nodesQuery.data?.length ?? 0) === 0 || testAllMutation.isPending
          }
        >
          {testAllMutation.isPending ? (
            <Loader2 className='size-4 animate-spin' />
          ) : (
            <TestTube2 className='size-4' />
          )}
          {t('Test All Nodes')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <ProxyNodeView />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
