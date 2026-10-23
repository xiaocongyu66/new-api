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
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import {
  SECURITY_OPTIONS,
  newPoolRowId,
  type SMTPPoolAccount,
} from './smtp-pool-model'

type SMTPPoolEditorProps = {
  accounts: SMTPPoolAccount[]
  onChange: (accounts: SMTPPoolAccount[]) => void
}

export function SMTPPoolEditor({ accounts, onChange }: SMTPPoolEditorProps) {
  const { t } = useTranslation()

  const update = (index: number, patch: Partial<SMTPPoolAccount>) => {
    onChange(
      accounts.map((account, i) =>
        i === index ? { ...account, ...patch } : account
      )
    )
  }

  return (
    <div className='flex flex-col gap-3'>
      {accounts.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'No accounts in the pool yet. Add one to send through multiple mailboxes.'
          )}
        </p>
      )}

      {accounts.map((account, index) => {
        let security = 'none'
        if (account.ssl) {
          security = 'ssl'
        } else if (account.starttls) {
          security = 'starttls'
        }
        return (
          <div
            key={account.id}
            className='grid gap-3 rounded-lg border p-3 md:grid-cols-[repeat(2,minmax(0,1fr))_auto]'
          >
            <div className='flex flex-col gap-1'>
              <label className='text-xs font-medium'>{t('SMTP server')}</label>
              <Input
                value={account.server}
                placeholder='smtp.qq.com'
                onChange={(event) =>
                  update(index, { server: event.target.value })
                }
              />
            </div>

            <div className='flex flex-col gap-1'>
              <label className='text-xs font-medium'>{t('Port')}</label>
              <Input
                value={account.port}
                inputMode='numeric'
                onChange={(event) =>
                  update(index, { port: event.target.value })
                }
              />
            </div>

            <div className='flex items-end'>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                aria-label={t('Remove account')}
                onClick={() => onChange(accounts.filter((_, i) => i !== index))}
              >
                <Trash2 className='size-4' />
              </Button>
            </div>

            <div className='flex flex-col gap-1'>
              <label className='text-xs font-medium'>{t('Account')}</label>
              <Input
                value={account.account}
                placeholder='name@qq.com'
                onChange={(event) =>
                  update(index, { account: event.target.value })
                }
              />
            </div>

            <div className='flex flex-col gap-1'>
              <label className='text-xs font-medium'>
                {t('Password / Access Token')}
              </label>
              <Input
                type='password'
                autoComplete='off'
                value={account.token}
                placeholder={t('Leave blank to keep the existing credential')}
                onChange={(event) =>
                  update(index, { token: event.target.value })
                }
              />
            </div>

            <div className='flex flex-col gap-1'>
              <label className='text-xs font-medium'>
                {t('From address (optional)')}
              </label>
              <Input
                value={account.from}
                placeholder={account.account || 'name@qq.com'}
                onChange={(event) =>
                  update(index, { from: event.target.value })
                }
              />
            </div>

            <div className='flex flex-col gap-1'>
              <label className='text-xs font-medium'>{t('Security')}</label>
              <Select
                value={security}
                onValueChange={(value) => {
                  const option = SECURITY_OPTIONS.find((o) => o.value === value)
                  if (!option) return
                  update(index, { ssl: option.ssl, starttls: option.starttls })
                }}
              >
                <SelectTrigger className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='starttls'>{t('STARTTLS')}</SelectItem>
                  <SelectItem value='ssl'>{t('SSL / TLS')}</SelectItem>
                  <SelectItem value='none'>{t('None')}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className='flex flex-col gap-1'>
              <label className='text-xs font-medium'>
                {t('Daily limit (0 = unlimited)')}
              </label>
              <Input
                value={account.dailyLimit}
                inputMode='numeric'
                placeholder='0'
                onChange={(event) =>
                  update(index, { dailyLimit: event.target.value })
                }
              />
            </div>

            <div className='flex flex-col gap-1'>
              <label className='text-xs font-medium'>{t('Priority')}</label>
              <Input
                value={account.priority}
                inputMode='numeric'
                placeholder='0'
                onChange={(event) =>
                  update(index, { priority: event.target.value })
                }
              />
            </div>
          </div>
        )
      })}

      <div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() =>
            onChange([
              ...accounts,
              {
                id: newPoolRowId(),
                server: '',
                port: '587',
                account: '',
                token: '',
                from: '',
                ssl: false,
                starttls: true,
                dailyLimit: '',
                priority: '0',
              },
            ])
          }
        >
          <Plus className='mr-1 size-4' />
          {t('Add account')}
        </Button>
      </div>
    </div>
  )
}
