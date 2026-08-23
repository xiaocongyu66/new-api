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
import { useState, useEffect, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  TableCaption,
} from '@/components/ui/table'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import { Loader2 } from 'lucide-react'

import { SettingsSection } from '../components/settings-section'
import { getRouteUnitAliases, getRouteUnits, updateRouteUnit, type RouteUnitAliasSummary, type RouteUnitView } from './api-route-units'
import { NumericSpinnerInput } from '@/features/channels/components/numeric-spinner-input'

interface RouteUnitsSectionProps {
  // No defaultValues needed - data comes from API
}

export function RouteUnitsSection({}: RouteUnitsSectionProps) {
  const { t } = useTranslation()
  const [aliases, setAliases] = useState<RouteUnitAliasSummary[]>([])
  const [selectedAlias, setSelectedAlias] = useState<string>('')
  const [routeUnits, setRouteUnits] = useState<RouteUnitView[]>([])
  const [totalWeight, setTotalWeight] = useState(0)
  const [loadingAliases, setLoadingAliases] = useState(true)
  const [loadingUnits, setLoadingUnits] = useState(false)
  const [savingIds, setSavingIds] = useState<Set<number>>(new Set())

  // Load aliases on mount
  useEffect(() => {
    const loadAliases = async () => {
      try {
        const data = await getRouteUnitAliases()
        setAliases(data)
        if (data.length > 0 && !selectedAlias) {
          setSelectedAlias(data[0].alias)
        }
      } catch (error) {
        toast.error(t('Failed to load route unit aliases'))
        console.error(error)
      } finally {
        setLoadingAliases(false)
      }
    }
    loadAliases()
  }, [t])

  // Load route units when alias changes
  useEffect(() => {
    if (!selectedAlias) {
      setRouteUnits([])
      setTotalWeight(0)
      return
    }

    let cancelled = false
    const loadUnits = async () => {
      setLoadingUnits(true)
      try {
        const data = await getRouteUnits(selectedAlias)
        if (!cancelled) {
          setRouteUnits(data.items)
          setTotalWeight(data.total_weight)
        }
      } catch (error) {
        if (!cancelled) {
          toast.error(t('Failed to load route units'))
          console.error(error)
        }
      } finally {
        if (!cancelled) {
          setLoadingUnits(false)
        }
      }
    }
    loadUnits()
    return () => {
      cancelled = true
    }
  }, [selectedAlias, t])

  const handleWeightChange = async (id: number, newWeight: number) => {
    // Capture original weight for potential rollback
    const unit = routeUnits.find(u => u.id === id)
    if (!unit) return
    const originalWeight = unit.static_weight

    // Debounce: only send PUT after 500ms of no further changes for this id
    const existingTimer = debounceTimers.current.get(id)
    if (existingTimer) {
      clearTimeout(existingTimer)
    }

    // Optimistic update immediately
    setRouteUnits(prev =>
      prev.map(u => (u.id === id ? { ...u, static_weight: newWeight } : u))
    )
    setTotalWeight(prev => prev - originalWeight + newWeight)

    // Mark as saving
    setSavingIds(prev => new Set(prev).add(id))

    const timer = setTimeout(async () => {
      debounceTimers.current.delete(id)
      try {
        await updateRouteUnit(id, { static_weight: newWeight })
        toast.success(t('Weight updated successfully'))
      } catch (error) {
        // Rollback only the affected row using captured original weight
        setRouteUnits(prev =>
          prev.map(u => (u.id === id ? { ...u, static_weight: originalWeight } : u))
        )
        // Recalc totalWeight from rolled-back state
        setTotalWeight(() => {
          const rolledBack = routeUnits.map(u => (u.id === id ? { ...u, static_weight: originalWeight } : u))
          return rolledBack.reduce((sum, u) => sum + u.static_weight, 0)
        })
        toast.error(t('Failed to update weight'))
        console.error(error)
      } finally {
        setSavingIds(prev => {
          const next = new Set(prev)
          next.delete(id)
          return next
        })
      }
    }, 500)

    debounceTimers.current.set(id, timer)
  }

  const handleEnabledChange = async (id: number, newEnabled: boolean) => {
    // Optimistic update
    setRouteUnits(prev => prev.map(u => (u.id === id ? { ...u, enabled: newEnabled } : u)))

    // Mark as saving
    setSavingIds(prev => new Set(prev).add(id))

    try {
      await updateRouteUnit(id, { enabled: newEnabled })
      toast.success(t('Status updated successfully'))
    } catch (error) {
      // Rollback only the affected row
      setRouteUnits(prev => prev.map(u => (u.id === id ? { ...u, enabled: u.enabled } : u)))
      toast.error(t('Failed to update status'))
      console.error(error)
    } finally {
      setSavingIds(prev => {
        const next = new Set(prev)
        next.delete(id)
        return next
      })
    }
  }

  const debounceTimers = useRef<Map<number, NodeJS.Timeout>>(new Map())

  // Flush pending debounced updates on unmount
  useEffect(() => {
    return () => {
      debounceTimers.current.forEach(timer => clearTimeout(timer))
      debounceTimers.current.clear()
    }
  }, [])

  const isSaving = (id: number) => savingIds.has(id)

  const expectedShare = useMemo(() => {
    return (weight: number) => {
      if (totalWeight === 0) return '0.00'
      return ((weight / totalWeight) * 100).toFixed(2)
    }
  }, [totalWeight])

  const channelStatusLabel = (status: number) => {
    switch (status) {
      case 1:
        return t('Active')
      case 2:
        return t('Disabled')
      default:
        return t('Unknown')
    }
  }

  const channelStatusClass = (status: number) => {
    switch (status) {
      case 1:
        return 'text-green-600 dark:text-green-400'
      case 2:
        return 'text-red-600 dark:text-red-400'
      default:
        return 'text-muted-foreground'
    }
  }

  if (loadingAliases) {
    return (
      <SettingsSection title={t('Route Units')}>
        <div className='flex items-center justify-center py-12'>
          <Loader2 className='h-6 w-6 animate-spin text-muted-foreground' />
          <span className='ml-2 text-muted-foreground'>{t('Loading aliases...')}</span>
        </div>
      </SettingsSection>
    )
  }

  if (aliases.length === 0) {
    return (
      <SettingsSection title={t('Route Units')}>
        <div className='text-center py-12 text-muted-foreground'>
          {t('No route unit aliases found')}
        </div>
      </SettingsSection>
    )
  }

  return (
    <SettingsSection title={t('Route Units')}>
      <div className='space-y-6'>
        {/* Alias Selector */}
        <div className='flex items-center gap-4'>
          <label htmlFor='route-unit-alias' className='text-sm font-medium'>
            {t('Public Model Alias')}
          </label>
          <Select value={selectedAlias} onValueChange={v => v && setSelectedAlias(v)}>
            <SelectTrigger id='route-unit-alias' className='w-[300px]'>
              <SelectValue placeholder={t('Select an alias')} />
            </SelectTrigger>
            <SelectContent>
              {aliases.map(alias => (
                <SelectItem key={alias.alias} value={alias.alias}>
                  {alias.alias} ({t('Total weight')}: {alias.total_weight}, {t('Routes')}: {alias.route_count})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {selectedAlias && (
          <>
            <Separator />

            {/* Route Units Table */}
            <div className='overflow-x-auto'>
              <Table>
                <TableCaption className='text-left text-sm text-muted-foreground'>
                  {t('Route units for')} <span className='font-mono'>{selectedAlias}</span>
                  {' '}({t('Total weight')}: {totalWeight})
                </TableCaption>
                <TableHeader>
                  <TableRow>
                    <TableHead className='w-[40px]'>{t('#')}</TableHead>
                    <TableHead>{t('Channel')}</TableHead>
                    <TableHead>{t('Key Index')}</TableHead>
                    <TableHead>{t('Upstream Model')}</TableHead>
                    <TableHead className='max-w-[200px] truncate'>{t('Base URL')}</TableHead>
                    <TableHead className='w-[100px]'>{t('Weight')}</TableHead>
                    <TableHead className='w-[120px]'>{t('Expected Share %')}</TableHead>
                    <TableHead className='w-[100px]'>{t('Enabled')}</TableHead>
                    <TableHead className='w-[100px]'>{t('Health Score')}</TableHead>
                    <TableHead className='w-[80px]'>{t('Status')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {loadingUnits ? (
                    <TableRow>
                      <TableCell colSpan={10} className='text-center py-8'>
                        <div className='flex items-center justify-center gap-2'>
                          <Loader2 className='h-5 w-5 animate-spin text-muted-foreground' />
                          <span>{t('Loading route units...')}</span>
                        </div>
                      </TableCell>
                    </TableRow>
                  ) : routeUnits.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={10} className='text-center py-8 text-muted-foreground'>
                        {t('No route units for this alias')}
                      </TableCell>
                    </TableRow>
                  ) : (
                    routeUnits.map((unit, index) => (
                      <TableRow key={unit.id}>
                        <TableCell className='font-mono text-muted-foreground'>{index + 1}</TableCell>
                        <TableCell className='font-medium'>{unit.channel_name}</TableCell>
                        <TableCell className='font-mono'>{unit.key_index}</TableCell>
                        <TableCell className='font-mono text-sm max-w-[150px] truncate' title={unit.upstream_model}>
                          {unit.upstream_model}
                        </TableCell>
                        <TableCell className='font-mono text-xs text-muted-foreground max-w-[200px] truncate' title={unit.base_url}>
                          {unit.base_url}
                        </TableCell>
                        <TableCell>
                          <NumericSpinnerInput
                            value={unit.static_weight}
                            onChange={value => handleWeightChange(unit.id, value)}
                            min={0}
                            step={1}
                            disabled={loadingUnits || isSaving(unit.id)}
                            className='w-[90px]'
                          />
                        </TableCell>
                        <TableCell className='font-mono text-sm text-muted-foreground'>
                          {expectedShare(unit.static_weight)}%
                        </TableCell>
                        <TableCell>
                          <Switch
                            checked={unit.enabled}
                            onCheckedChange={checked => handleEnabledChange(unit.id, checked)}
                            disabled={loadingUnits || isSaving(unit.id)}
                            aria-label={t(unit.enabled ? 'Disable route unit' : 'Enable route unit')}
                          />
                        </TableCell>
                        <TableCell className='font-mono text-sm'>
                          {unit.health_score.toFixed(2)}
                        </TableCell>
                        <TableCell>
                          <span className={channelStatusClass(unit.channel_status)}>
                            {channelStatusLabel(unit.channel_status)}
                          </span>
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>
          </>
        )}
      </div>
    </SettingsSection>
  )
}