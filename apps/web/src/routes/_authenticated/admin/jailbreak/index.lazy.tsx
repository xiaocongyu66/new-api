import { createLazyFileRoute } from '@tanstack/react-router'

import { Jailbreak } from '@/features/jailbreak'

export const Route = createLazyFileRoute('/_authenticated/admin/jailbreak/')({
  component: Jailbreak,
})