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
import { BalanceQueryDialog } from './dialogs/balance-query-dialog'
import { CopyChannelDialog } from './dialogs/copy-channel-dialog'
import { EditTagDialog } from './dialogs/edit-tag-dialog'
import { FetchModelsDialog } from './dialogs/fetch-models-dialog'
import { MultiKeyManageDialog } from './dialogs/multi-key-manage-dialog'
import { OllamaModelsDialog } from './dialogs/ollama-models-dialog'
import { RouteHealthDialog } from './dialogs/route-health-dialog'
import { TagBatchEditDialog } from './dialogs/tag-batch-edit-dialog'
import { UpstreamUpdateDialog } from './dialogs/upstream-update-dialog'

const LazyChannelMutateDrawer = lazy(() =>
  import('./drawers/channel-mutate-drawer').then((module) => ({
    default: module.ChannelMutateDrawer,
  }))
)

const LazyChannelTestDialog = lazy(() =>
  import('./dialogs/channel-test-dialog').then((module) => ({
    default: module.ChannelTestDialog,
  }))
)

export function ChannelsDialogs() {
  const { open, setOpen, currentRow, upstream } = useChannels()

  return (
    <>
      {/* Channel editor drawer for existing channels */}
      <Suspense fallback={null}>
        <LazyChannelMutateDrawer
          open={open === 'update-channel'}
          onOpenChange={(v) => !v && setOpen(null)}
          currentRow={currentRow}
        />
      </Suspense>

      {/* Test Channel Dialog */}
      <Suspense fallback={null}>
        <LazyChannelTestDialog
          open={open === 'test-channel'}
          onOpenChange={(v) => !v && setOpen(null)}
        />
      </Suspense>

      {/* Balance Query Dialog */}
      <BalanceQueryDialog
        open={open === 'balance-query'}
        onOpenChange={(v) => !v && setOpen(null)}
      />

      {/* Fetch Models Dialog */}
      <FetchModelsDialog
        open={open === 'fetch-models'}
        onOpenChange={(v) => !v && setOpen(null)}
      />

      {/* Ollama Models Dialog */}
      <OllamaModelsDialog
        open={open === 'ollama-models'}
        onOpenChange={(v) => !v && setOpen(null)}
      />

      {/* Copy Channel Dialog */}
      <CopyChannelDialog
        open={open === 'copy-channel'}
        onOpenChange={(v) => !v && setOpen(null)}
      />

      {/* Multi-Key Management Dialog */}
      <MultiKeyManageDialog
        open={open === 'multi-key-manage'}
        onOpenChange={(v) => !v && setOpen(null)}
      />

      {/* Tag Batch Edit Dialog */}
      <TagBatchEditDialog
        open={open === 'tag-batch-edit'}
        onOpenChange={(v) => !v && setOpen(null)}
      />

      {/* Edit Tag Dialog */}
      <EditTagDialog
        open={open === 'edit-tag'}
        onOpenChange={(v) => !v && setOpen(null)}
      />

      {/* Route Health Dialog */}
      <RouteHealthDialog
        open={open === 'route-health'}
        onOpenChange={(v) => !v && setOpen(null)}
      />

      {/* Upstream Model Update Dialog */}
      <UpstreamUpdateDialog
        open={upstream.showModal}
        addModels={upstream.addModels}
        removeModels={upstream.removeModels}
        preferredTab={upstream.preferredTab}
        confirmLoading={upstream.applyLoading}
        onConfirm={upstream.applyUpdates}
        onCancel={upstream.closeModal}
      />
    </>
  )
}
