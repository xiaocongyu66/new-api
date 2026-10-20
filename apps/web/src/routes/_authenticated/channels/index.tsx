import { createFileRoute, redirect } from '@tanstack/react-router'

import { CHANNELS_DEFAULT_SECTION } from '@/features/channels/section-registry'

export const Route = createFileRoute('/_authenticated/channels/')({
  beforeLoad: ({ location }) => {
    throw redirect({
      to: '/admin/channels/$section',
      params: { section: CHANNELS_DEFAULT_SECTION },
      search: location.search,
      hash: location.hash,
    })
  },
})
