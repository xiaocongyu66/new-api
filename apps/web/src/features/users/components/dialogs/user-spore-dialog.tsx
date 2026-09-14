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
import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  formatSpore,
  parseSporeToUnits,
  sporeUnitsToValue,
} from '@/lib/spore'

import { adjustUserSpore } from '../../api'
import type { QuotaAdjustMode, User } from '../../types'

const sporeFormSchema = (t: (key: string) => string) =>
  z
    .object({
      mode: z.enum(['add', 'subtract', 'override']),
      // 字符串保存输入中间态（"1." 这类），避免受控 number 输入吃掉小数点。
      amount: z.string(),
    })
    .superRefine((data, ctx) => {
      const parsed = Number.parseFloat(data.amount)
      if (!Number.isFinite(parsed)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['amount'],
          message: t('Amount must be a valid number'),
        })
        return
      }
      // override 允许归零，add/subtract 至少 0.1 菌种。
      const min = data.mode === 'override' ? 0 : 0.1
      if (parsed < min) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['amount'],
          message: t('Amount must be at least 0.1'),
        })
      }
    })

type SporeFormValues = z.infer<typeof sporeFormSchema>

interface UserSporeDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: User | null
  onSuccess?: () => void
}

export function UserSporeDialog({
  open,
  onOpenChange,
  user,
  onSuccess,
}: UserSporeDialogProps) {
  const { t } = useTranslation()
  const [submitting, setSubmitting] = useState(false)

  const form = useForm<SporeFormValues>({
    resolver: zodResolver(sporeFormSchema(t)),
    defaultValues: {
      mode: 'add',
      amount: '',
    },
  })

  if (!user) return null

  const currentSporeUnits = user.spore ?? 0

  const onSubmit = async (values: SporeFormValues) => {
    setSubmitting(true)
    try {
      const units = parseSporeToUnits(values.amount)
      const res = await adjustUserSpore({
        id: user.id,
        action: 'add_spore',
        mode: values.mode as QuotaAdjustMode,
        value: units,
      })
      if (res.success) {
        toast.success(t('Spore adjusted successfully'))
        onOpenChange(false)
        form.reset()
        onSuccess?.()
      } else {
        toast.error(res.message || t('Failed to adjust spore'))
      }
    } catch {
      toast.error(t('Network error'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        // 取消/遮罩关闭时清掉残留的金额与模式，避免下次打开误提交。
        if (!o) form.reset()
        onOpenChange(o)
      }}
    >
      <DialogContent className='sm:max-w-[425px]'>
        <DialogHeader>
          <DialogTitle>{t('Adjust user spore')}</DialogTitle>
          <DialogDescription>
            {t('Manage voucher spore balance for')} {user.username}
          </DialogDescription>
        </DialogHeader>

        <div className='bg-muted/50 rounded-lg p-3 text-sm'>
          <span className='text-muted-foreground'>
            {t('Current spore balance')}:
          </span>{' '}
          <span className='font-mono font-semibold'>
            {formatSpore(currentSporeUnits)}
          </span>
        </div>

        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-4'>
            <FormField
              control={form.control}
              name='mode'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Mode')}</FormLabel>
                  <Select
                    items={[
                      { value: 'add', label: t('Add') },
                      { value: 'subtract', label: t('Subtract') },
                      { value: 'override', label: t('Override') },
                    ]}
                    value={field.value}
                    onValueChange={(val) => {
                      field.onChange(val)
                      // Override 回填当前余额；切走时清空，防止残留旧金额误提交。
                      form.setValue(
                        'amount',
                        val === 'override'
                          ? String(sporeUnitsToValue(currentSporeUnits))
                          : ''
                      )
                    }}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value='add'>{t('Add')}</SelectItem>
                      <SelectItem value='subtract'>{t('Subtract')}</SelectItem>
                      <SelectItem value='override'>{t('Override')}</SelectItem>
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='amount'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Amount (spore)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      step='0.1'
                      placeholder='1.0'
                      value={field.value}
                      onChange={field.onChange}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Precision is 0.1 spore')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <DialogFooter>
              <Button
                type='button'
                variant='outline'
                onClick={() => onOpenChange(false)}
                disabled={submitting}
              >
                {t('Cancel')}
              </Button>
              <Button type='submit' disabled={submitting}>
                {submitting ? t('Submitting...') : t('Confirm')}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}
