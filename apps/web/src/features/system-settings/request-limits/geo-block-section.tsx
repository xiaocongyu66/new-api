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
import { useEffect, useMemo, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const geoBlockSchema = z.object({
  geo_block_setting: z.object({
    enabled: z.boolean(),
    blocked_countries: z.string(),
  }),
})

type GeoBlockFormValues = z.output<typeof geoBlockSchema>
type GeoBlockFormInput = z.input<typeof geoBlockSchema>

type NormalizedGeoBlockValues = {
  'geo_block_setting.enabled': boolean
  'geo_block_setting.blocked_countries': string[]
}

type GeoBlockSectionProps = {
  defaultValues: {
    'geo_block_setting.enabled': boolean
    'geo_block_setting.blocked_countries': string[]
  }
}

const splitLines = (value: string) =>
  value
    .split('\n')
    .map((entry) => entry.trim().toUpperCase())
    .filter(Boolean)

const buildFormDefaults = (
  defaults: GeoBlockSectionProps['defaultValues']
): GeoBlockFormInput => ({
  geo_block_setting: {
    enabled: defaults['geo_block_setting.enabled'],
    blocked_countries: defaults['geo_block_setting.blocked_countries'].join(
      '\n'
    ),
  },
})

const normalizeDefaults = (
  defaults: GeoBlockSectionProps['defaultValues']
): NormalizedGeoBlockValues => ({
  'geo_block_setting.enabled': defaults['geo_block_setting.enabled'],
  'geo_block_setting.blocked_countries':
    defaults['geo_block_setting.blocked_countries'],
})

const normalizeFormValues = (
  values: GeoBlockFormValues
): NormalizedGeoBlockValues => ({
  'geo_block_setting.enabled': values.geo_block_setting.enabled,
  'geo_block_setting.blocked_countries': splitLines(
    values.geo_block_setting.blocked_countries
  ),
})

const isEqual = (a: unknown, b: unknown) => {
  if (Array.isArray(a) && Array.isArray(b)) {
    return JSON.stringify(a) === JSON.stringify(b)
  }
  return a === b
}

export function GeoBlockSection({ defaultValues }: GeoBlockSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const baselineRef = useRef<NormalizedGeoBlockValues>(
    normalizeDefaults(defaultValues)
  )

  const formDefaults = useMemo(
    () => buildFormDefaults(defaultValues),
    [defaultValues]
  )

  const form = useForm<GeoBlockFormInput, unknown, GeoBlockFormValues>({
    resolver: zodResolver(geoBlockSchema),
    defaultValues: formDefaults,
  })

  useEffect(() => {
    baselineRef.current = normalizeDefaults(defaultValues)
    form.reset(buildFormDefaults(defaultValues))
  }, [defaultValues, form])

  const computePendingUpdates = (
    data: GeoBlockFormValues
  ): Array<[string, boolean | string[]]> => {
    const normalized = normalizeFormValues(data)
    return (Object.keys(normalized) as Array<
      keyof NormalizedGeoBlockValues
    >)
      .filter((key) => !isEqual(normalized[key], baselineRef.current[key]))
      .map((key) => [key, normalized[key]] as [string, boolean | string[]])
  }

  const handleBaselineRef = (normalized: NormalizedGeoBlockValues) => {
    baselineRef.current = normalized
    form.reset(buildFormDefaults(normalized))
  }

  const onSubmit = async (data: GeoBlockFormValues) => {
    const pending = computePendingUpdates(data)

    if (pending.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    try {
      for (const [key, value] of pending) {
        await updateOption.mutateAsync({
          key,
          value: Array.isArray(value) ? JSON.stringify(value) : value,
        })
      }
    } catch {
      // react-query surfaces the failure via onError toast; do not
      // touch the baseline when a write failed, so a retry resends it.
      return
    }

    handleBaselineRef(normalizeFormValues(data))
    toast.success(t('Saved'))
  }

  return (
    <SettingsSection title={t('Geographic Blocking')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || form.formState.isSubmitting}
          />

          <FormField
            control={form.control}
            name='geo_block_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Block requests from selected countries')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Matches the client IP against a local GeoIP database (MaxMind DB) and rejects requests whose country is on the blocklist. The database file path is read from the GEOIP_DB_PATH environment variable; when it is missing or unreadable the check is skipped and requests are allowed.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='geo_block_setting.blocked_countries'
            render={({ field }) => (
              <FormItem className='flex flex-col gap-2'>
                <FormLabel>
                  {t('Blocked countries (ISO 3166-1 codes, one per line)')}
                </FormLabel>
                <FormControl>
                  <Textarea
                    rows={4}
                    placeholder="CN"
                    value={field.value}
                    onChange={field.onChange}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Requests whose IP resolves to one of these countries are rejected with a 403. Case-insensitive.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
