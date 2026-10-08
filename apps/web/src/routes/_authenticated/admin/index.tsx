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
import { createFileRoute, Link, redirect } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { getVisibleAdminNavigation } from '@/components/layout/config/admin-navigation.config'
import { AdminDashboard } from '@/features/admin-dashboard'
import { hasPermission } from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/admin/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()

    if (!auth.user || auth.user.role < ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  component: AdminEntry,
})

function AdminEntry() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const adminLinks = getVisibleAdminNavigation(user, hasPermission).filter(
    (item) => item.id !== 'overview'
  )

  return (
    <main className='flex min-h-full flex-1 flex-col gap-6 p-4 md:p-6'>
      <AdminDashboard />
      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
        {adminLinks.map((item) => (
          <Link
            key={item.id}
            to={item.to as never}
            params={item.params as never}
            search={item.search as never}
            className='bg-card-surface backdrop-blur-card hover:bg-accent rounded-lg border p-4 transition-colors'
          >
            <span className='font-medium'>{t(item.labelKey)}</span>
          </Link>
        ))}
      </div>
    </main>
  )
}
