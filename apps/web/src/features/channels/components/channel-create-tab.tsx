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
import { lazy, Suspense } from 'react'

import { useChannels } from './channels-provider'

const LazyChannelMutateDrawer = lazy(() =>
  import('./drawers/channel-mutate-drawer').then((module) => ({
    default: module.ChannelMutateDrawer,
  }))
)

export function ChannelCreateTab(props: {
  actionsContainer: HTMLElement | null
  active: boolean
}) {
  const { setPageTab } = useChannels()

  return (
    <div className='flex min-h-full flex-col'>
      <Suspense fallback={<div className='flex min-h-0 flex-1' />}>
        <LazyChannelMutateDrawer
          open={props.active}
          presentation='inline'
          currentRow={null}
          formId='channel-create-form'
          actionsContainer={props.actionsContainer}
          onOpenChange={(isOpen) => {
            if (!isOpen) setPageTab('channels')
          }}
        />
      </Suspense>
    </div>
  )
}
