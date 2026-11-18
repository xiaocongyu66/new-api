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
import type { ChangeEvent } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import type { Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
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

      <Alert>
        <AlertDescription>
          {t(
            'Rebates apply only to wallet top-ups; subscription purchases are not included.'
          )}
        </AlertDescription>
      </Alert>

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
                        formattedQuota: formatQuota(Number(field.value) || 0),
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
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Quota given to invited users ({{formattedQuota}})', {
                      formattedQuota: formatQuota(Number(field.value) || 0),
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
          </SettingsFormGrid>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
