import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/channels/')({
  beforeLoad: ({ location }) => {
    throw redirect({
      to: '/admin/channels',
      search: location.search,
      hash: location.hash,
    })
  },
})
