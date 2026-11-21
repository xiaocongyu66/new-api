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
import { useEffect, useState } from 'react'
import type { ChangeEvent } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import type { Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { MultiSelect } from '@/components/multi-select'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
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
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { formatQuota } from '@/lib/format'

import { getRedemptionNames } from '@/features/redemption-codes/api'
import type { RedemptionNameGroup } from '@/features/redemption-codes/types'

import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import {
  SettingsForm,
  SettingsFormGrid,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

const createInvitationRewardSchema = () =>
  z.object({
    QuotaForInviter_display: z.coerce.number().min(0),
    QuotaForInvitee_display: z.coerce.number().min(0),
    invite_reward: z.object({
      inviter_mode: z.enum(['direct', 'recharge']),
      rebate_multiplier: z.coerce.number().min(0),
      rebate_trigger: z.enum(['first', 'every']),
      transfer_min_quota_display: z.coerce.number().min(0),
      recharge_sources: z.string(),
      redemption_code_names: z.string(),
    }),
  })

type InvitationRewardFormValues = {
  QuotaForInviter_display: number
  QuotaForInvitee_display: number
  invite_reward: {
    inviter_mode: 'direct' | 'recharge'
    rebate_multiplier: number
    rebate_trigger: 'first' | 'every'
    transfer_min_quota_display: number
    recharge_sources: string
    redemption_code_names: string
  }
}
type QuotaInputValue = number | ''

type InvitationRewardSettingsSectionProps = {
  defaultValues: InvitationRewardFormValues
  complianceConfirmed?: boolean
}

export function InvitationRewardSettingsSection(
  props: InvitationRewardSettingsSectionProps
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const complianceConfirmed = props.complianceConfirmed ?? true
  const [redemptionNames, setRedemptionNames] = useState<RedemptionNameGroup[]>([])

  useEffect(() => {
    let active = true
    void getRedemptionNames()
      .then((response) => {
        if (!active || !response.success || !response.data) return
        setRedemptionNames(response.data)
      })
      .catch(() => undefined)
    return () => {
      active = false
    }
  }, [])


  const handleNumberChange =
    (onChange: (value: QuotaInputValue) => void) =>
    (event: ChangeEvent<HTMLInputElement>) => {
      const value = event.currentTarget.valueAsNumber
      onChange(Number.isNaN(value) ? '' : value)
    }

  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<InvitationRewardFormValues>({
      resolver: zodResolver(createInvitationRewardSchema()) as Resolver<
        InvitationRewardFormValues,
        unknown,
        InvitationRewardFormValues
      >,
      defaultValues: props.defaultValues,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          await updateOption.mutateAsync({
            key,
            value: value as string | number | boolean,
          })
        }
      },
    })

  // Base UI renders SelectValue from `items`, so translated labels appear in
  // the closed trigger as well as in the open list.
  const rewardModeItems = [
    { value: 'direct', label: t('Direct Grant') },
    { value: 'recharge', label: t('Recharge Rebate') },
  ]
  const rebateTriggerItems = [
    { value: 'first', label: t('First Recharge Only') },
    { value: 'every', label: t('Every Recharge') },
  ]

  const sourceItems = [
    { value: 'topup', label: t('Wallet top-up') },
    { value: 'subscription', label: t('Subscription purchase') },
    { value: 'redemption', label: t('Redemption codes') },
  ]

  return (
    <SettingsSection title={t('Invitation Rewards')}>
      <FormNavigationGuard when={isDirty} />

      {!complianceConfirmed ? (
        <Alert variant='destructive'>
          <AlertDescription>
            {t(
              'Non-zero invitation rewards require compliance confirmation in Payment Gateway settings.'
            )}
          </AlertDescription>
        </Alert>
      ) : null}


      <Form {...form}>
        <SettingsForm onSubmit={handleSubmit}>
          <SettingsPageFormActions
            onSave={handleSubmit}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!complianceConfirmed}
          />
          <FormDirtyIndicator isDirty={isDirty} />

          <SettingsFormGrid className='grid-cols-2 gap-x-3'>
            <FormField
              control={form.control}
              name='QuotaForInviter_display'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Inviter Reward')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Quota given to users who invite others ({{formattedQuota}})',
                      {
                        formattedQuota: formatQuota(Math.max(0, Number(field.value) || 0)),
                      }
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='QuotaForInvitee_display'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Invitee Reward')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Quota given to invited users ({{formattedQuota}})', {
                      formattedQuota: formatQuota(Math.max(0, Number(field.value) || 0)),
                    })}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='invite_reward.inviter_mode'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Inviter Reward Method')}</FormLabel>
                  <FormControl>
                    <Select
                      items={rewardModeItems}
                      value={field.value ?? 'direct'}
                      onValueChange={(value) =>
                        field.onChange(
                          (value ?? 'direct') as 'direct' | 'recharge'
                        )
                      }
                    >
                      <SelectTrigger>
                        <SelectValue>
                          {rewardModeItems.find((item) => item.value === field.value)?.label ??
                            t('Direct Grant')}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {rewardModeItems.map((item) => (
                            <SelectItem key={item.value} value={item.value}>
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='invite_reward.rebate_multiplier'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Rebate Multiplier')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      step={0.1}
                      min={0}
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('A multiplier of 0 means no rebate; set it above 0 to enable recharge rebates.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='invite_reward.rebate_trigger'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Rebate Trigger')}</FormLabel>
                  <FormControl>
                    <Select
                      items={rebateTriggerItems}
                      value={field.value ?? 'first'}
                      onValueChange={(value) =>
                        field.onChange((value ?? 'first') as 'first' | 'every')
                      }
                    >
                      <SelectTrigger>
                        <SelectValue>
                          {rebateTriggerItems.find((item) => item.value === field.value)?.label ??
                            t('First Recharge Only')}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {rebateTriggerItems.map((item) => (
                            <SelectItem key={item.value} value={item.value}>
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='invite_reward.transfer_min_quota_display'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Minimum Transfer Amount')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>{t('0 = no extra minimum')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='invite_reward.recharge_sources'
              render={({ field }) => {
                const selected = (field.value ?? '').split(',').filter(Boolean)
                return (
                  <FormItem className='col-span-2'>
                    <FormLabel>{t('Recharge Sources')}</FormLabel>
                    <FormControl>
                      <div
                        role='group'
                        aria-label={t('Recharge Sources')}
                        className='flex flex-wrap gap-2'
                      >
                        {sourceItems.map((item) => {
                          const pressed = selected.includes(item.value)
                          return (
                            <Button
                              key={item.value}
                              type='button'
                              variant={pressed ? 'default' : 'outline'}
                              size='lg'
                              aria-pressed={pressed}
                              disabled={pressed && selected.length === 1}
                              className='disabled:opacity-100'
                              onClick={() => {
                                const next = pressed
                                  ? selected.filter((value) => value !== item.value)
                                  : [...selected, item.value]
                                field.onChange(next.join(','))
                              }}
                            >
                              {item.label}
                            </Button>
                          )
                        })}
                      </div>
                    </FormControl>
                    <FormDescription>
                      {t('Select at least one recharge source')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )
              }}
            />

            {form
              .watch('invite_reward.recharge_sources')
              ?.split(',')
              .includes('redemption') ? (
              <FormField
                control={form.control}
                name='invite_reward.redemption_code_names'
                render={({ field }) => {
                  const selected = (field.value ?? '')
                    .split(',')
                    .filter(Boolean)
                  return (
                    <FormItem className='col-span-2'>
                      <FormLabel>
                        {t('Redemption code types counted as recharge')}
                      </FormLabel>
                      <FormControl>
                        <MultiSelect
                          options={redemptionNames.map((group) => ({
                            label: `${group.name} · ${t('{{count}} codes', { count: String(group.count) })} · ${t('Total {{amount}}', { amount: formatQuota(group.total_quota_display) })}`,
                            value: group.name,
                          }))}
                          selected={selected}
                          onChange={(values) => field.onChange(values.join(','))}
                          placeholder={t('Search or add redemption code types')}
                          allowCreate
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Codes are grouped by name. New codes with a selected name will count as recharge automatically.')}
                      </FormDescription>
                      {selected.length === 0 ? (
                        <FormDescription>
                          {t('No types selected, redemption codes will not count as recharge')}
                        </FormDescription>
                      ) : null}
                      <FormMessage />
                    </FormItem>
                  )
                }}
              />
            ) : null}
          </SettingsFormGrid>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
