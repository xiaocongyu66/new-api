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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import dayjs from '@/lib/dayjs'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Button } from '@/components/ui/button'
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
import {
  getGeoipDatabaseStatus,
  updateGeoipDatabase,
} from '../api'
import type { GeoipDatabaseStatus } from '../types'

const geoBlockSchema = z.object({
  geo_block_setting: z.object({
    enabled: z.boolean(),
    allow_admin: z.boolean(),
    blocked_countries: z.string(),
  }),
})

type GeoBlockFormValues = z.output<typeof geoBlockSchema>
type GeoBlockFormInput = z.input<typeof geoBlockSchema>

type NormalizedGeoBlockValues = {
  'geo_block_setting.enabled': boolean
  'geo_block_setting.allow_admin': boolean
  'geo_block_setting.blocked_countries': string[]
}

type GeoBlockSectionProps = {
  defaultValues: {
    'geo_block_setting.enabled': boolean
    'geo_block_setting.allow_admin': boolean
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
    allow_admin: defaults['geo_block_setting.allow_admin'],
    blocked_countries: defaults['geo_block_setting.blocked_countries'].join(
      '\n'
    ),
  },
})

const normalizeDefaults = (
  defaults: GeoBlockSectionProps['defaultValues']
): NormalizedGeoBlockValues => ({
  'geo_block_setting.enabled': defaults['geo_block_setting.enabled'],
  'geo_block_setting.allow_admin': defaults['geo_block_setting.allow_admin'],
  'geo_block_setting.blocked_countries':
    defaults['geo_block_setting.blocked_countries'],
})

const normalizeFormValues = (
  values: GeoBlockFormValues
): NormalizedGeoBlockValues => ({
  'geo_block_setting.enabled': values.geo_block_setting.enabled,
  'geo_block_setting.allow_admin': values.geo_block_setting.allow_admin,
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

function formatDatabaseSize(sizeBytes: number): string {
  if (sizeBytes >= 1048576) {
    return `${(sizeBytes / 1048576).toFixed(1)} MB`
  }
  return `${Math.max(1, Math.round(sizeBytes / 1024))} KB`
}

export function GeoBlockSection({ defaultValues }: GeoBlockSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()

  const database = useQuery({
    queryKey: ['geoip-database'],
    queryFn: getGeoipDatabaseStatus,
  })
  const dbStatus: GeoipDatabaseStatus | undefined = database.data?.data

  const updateDatabase = useMutation({
    mutationFn: updateGeoipDatabase,
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['geoip-database'] })
      if (res.success) {
        toast.success(t('GeoIP database updated'))
      } else {
        toast.error(res.message || t('Failed to update GeoIP database'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to update GeoIP database'))
    },
  })

  const databaseStatusText = (() => {
    if (!dbStatus) return '-'
    if (dbStatus.externally_managed) {
      return dbStatus.exists
        ? t('Managed externally via GEOIP_DB_PATH')
        : t('Managed externally via GEOIP_DB_PATH, but the file is missing')
    }
    if (!dbStatus.exists) return t('Not downloaded yet')
    if (dbStatus.stale) {
      return t('Stale (older than {{days}} days)', {
        days: dbStatus.fresh_window_days,
      })
    }
    return t('Up to date')
  })()
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

          <div className='rounded-lg border p-4 text-sm'>
            <div className='mb-3 flex items-center justify-between gap-2'>
              <FormLabel>{t('GeoIP database')}</FormLabel>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={
                  updateDatabase.isPending || Boolean(dbStatus?.externally_managed)
                }
                onClick={() => updateDatabase.mutate()}
              >
                {updateDatabase.isPending ? t('Updating...') : t('Update now')}
              </Button>
            </div>
            <dl className='grid gap-x-6 gap-y-1.5 md:grid-cols-[10rem_1fr]'>
              <dt className='text-muted-foreground'>{t('Status')}</dt>
              <dd>{databaseStatusText}</dd>
              <dt className='text-muted-foreground'>{t('Last updated')}</dt>
              <dd>
                {dbStatus?.updated_at
                  ? `${dayjs(dbStatus.updated_at * 1000).format('YYYY-MM-DD HH:mm:ss')} (${dayjs(dbStatus.updated_at * 1000).fromNow()})` // backend sends Unix seconds
                  : '-'}
              </dd>
              <dt className='text-muted-foreground'>{t('Size')}</dt>
              <dd>
                {dbStatus?.exists ? formatDatabaseSize(dbStatus.size_bytes) : '-'}
              </dd>
              <dt className='text-muted-foreground'>{t('File')}</dt>
              <dd className='truncate' title={dbStatus?.path}>
                {dbStatus?.path || '-'}
              </dd>
              <dt className='text-muted-foreground'>{t('Download source')}</dt>
              <dd className='truncate' title={dbStatus?.source_url}>
                {dbStatus?.source_url || '-'}
              </dd>
            </dl>
          </div>

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
            name='geo_block_setting.allow_admin'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Allow administrators')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Administrators (and root) can still sign in and use the site from blocked countries, so the operator is never locked out. Visitors and regular users stay blocked.'
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
                    'Requests whose IP resolves to one of these countries see a 404 page instead of the site. Case-insensitive.'
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
