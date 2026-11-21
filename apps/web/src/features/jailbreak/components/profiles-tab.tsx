import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import {
  deleteJailbreakProfile,
  getJailbreakProfiles,
  updateJailbreakProfile,
} from '../api'
import type { JailbreakProfile } from '../types'
import { ProfileDrawer } from './profile-drawer'

export function ProfilesTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState('')
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [editing, setEditing] = useState<JailbreakProfile | null>(null)
  const [deleting, setDeleting] = useState<JailbreakProfile | null>(null)

  const { data: profiles = [], isLoading } = useQuery({
    queryKey: ['jailbreak-profiles'],
    queryFn: getJailbreakProfiles,
  })

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ['jailbreak-profiles'] })

  const toggleMutation = useMutation({
    mutationFn: (profile: JailbreakProfile) =>
      updateJailbreakProfile(profile.id, {
        name: profile.name,
        description: profile.description,
        kind: profile.kind,
        prompt: profile.prompt,
        headers: profile.headers,
        mode: profile.mode,
        priority: profile.priority,
        enabled: !profile.enabled,
        channels: profile.channels,
        models: profile.models,
        channel_models: profile.channel_models,
      }),
    onSuccess: () => invalidate(),
    onError: handleServerError,
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteJailbreakProfile(id),
    onSuccess: () => {
      toast.success(t('Deleted'))
      invalidate()
      setDeleting(null)
    },
    onError: handleServerError,
  })

  const keyword = filter.trim().toLowerCase()
  const visible = keyword
    ? profiles.filter(
        (profile) =>
          profile.name.toLowerCase().includes(keyword) ||
          profile.description.toLowerCase().includes(keyword) ||
          profile.prompt.toLowerCase().includes(keyword) ||
          profile.headers.toLowerCase().includes(keyword)
      )
    : profiles

  const typeBadge = (profile: JailbreakProfile) => (
    <Badge variant={profile.kind === 'header' ? 'warning' : 'default'}>
      {t(profile.kind === 'header' ? 'Header Modification' : 'Prompt Injection')}
    </Badge>
  )

  const modeBadge = (profile: JailbreakProfile) =>
    profile.kind !== 'prompt' ? null : (
      <>
        <Badge
          variant={
            profile.mode === 'replace'
              ? 'warning'
              : profile.mode === 'append'
                ? 'secondary'
                : 'default'
          }
        >
          {t(
            profile.mode === 'replace'
              ? 'Replace'
              : profile.mode === 'append'
                ? 'Append'
                : 'Prepend'
          )}
        </Badge>
      </>
    )

  return (
    <div className='h-full min-h-0 space-y-4 overflow-y-auto pb-24'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <Input
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
          placeholder={t('Search rules or prompts')}
          className='w-full sm:w-72'
        />
        <Button
          onClick={() => {
            setEditing(null)
            setDrawerOpen(true)
          }}
        >
          <Plus className='size-4' />
          {t('Create Request Rule')}
        </Button>
      </div>

      <Card className='py-0'>
        <CardContent className='hidden p-0 lg:block'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Rule')}</TableHead>
                <TableHead>{t('Type')}</TableHead>
                <TableHead className='hidden sm:table-cell'>{t('Mode')}</TableHead>
                <TableHead>{t('Bindings')}</TableHead>
                <TableHead className='hidden xl:table-cell'>{t('Updated')}</TableHead>
                <TableHead className='text-end'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={6} className='text-muted-foreground py-10 text-center'>
                    {t('Loading...')}
                  </TableCell>
                </TableRow>
              ) : visible.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className='text-muted-foreground py-10 text-center'>
                    {t('No jailbreak prompts yet')}
                  </TableCell>
                </TableRow>
              ) : (
                visible.map((profile) => (
                  <TableRow
                    key={profile.id}
                    tabIndex={0}
                    role='button'
                    aria-label={t('Edit Request Rule')}
                    onClick={() => {
                      setEditing(profile)
                      setDrawerOpen(true)
                    }}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault()
                        setEditing(profile)
                        setDrawerOpen(true)
                      }
                    }}
                    className={`cursor-pointer outline-none focus-visible:bg-muted ${
                      profile.enabled ? '' : 'opacity-60'
                    }`}
                  >
                    <TableCell className='max-w-[280px] whitespace-normal'>
                      <div className='font-medium'>{profile.name}</div>
                      {profile.description ? (
                        <div className='text-muted-foreground mt-0.5 text-xs break-words'>
                          {profile.description}
                        </div>
                      ) : null}
                      <div className='text-muted-foreground mt-1 line-clamp-1 font-mono text-xs break-words'>
                        {profile.kind === 'header'
                          ? (profile.headers || '{}')
                          : profile.prompt}
                      </div>
                    </TableCell>
                    <TableCell>{typeBadge(profile)}</TableCell>
                    <TableCell className='hidden sm:table-cell'>
                      {modeBadge(profile)}
                    </TableCell>
                    <TableCell>
                      <div className='flex flex-wrap gap-1'>
                        <Badge variant='outline'>
                          {t('Channels')}{' '}
                          {Object.keys(profile.channel_models ?? {}).length}
                        </Badge>
                        <Badge variant='outline'>
                          {t('Models')}{' '}
                          {Object.values(profile.channel_models ?? {}).reduce(
                            (total, names) => total + names.length,
                            0
                          )}
                        </Badge>
                      </div>
                    </TableCell>
                    <TableCell className='text-muted-foreground hidden text-xs xl:table-cell'>
                      {formatTimestampToDate(profile.updated_time)}
                    </TableCell>
                    <TableCell>
                      <div className='flex items-center justify-end gap-1'>
                        <Switch
                          checked={profile.enabled}
                          onCheckedChange={() => toggleMutation.mutate(profile)}
                          onClick={(event) => event.stopPropagation()}
                          aria-label={t('Enabled')}
                        />
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          aria-label={t('Delete')}
                          onClick={(event) => {
                            event.stopPropagation()
                            setDeleting(profile)
                          }}
                        >
                          <Trash2 className='size-4 text-destructive' />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </CardContent>
        <CardContent className='space-y-3 p-3 lg:hidden'>
          {isLoading ? (
            <p className='text-muted-foreground py-6 text-center text-sm'>
              {t('Loading...')}
            </p>
          ) : visible.length === 0 ? (
            <p className='text-muted-foreground py-6 text-center text-sm'>
              {t('No jailbreak prompts yet')}
            </p>
          ) : (
            visible.map((profile) => (
              <div
                key={profile.id}
                tabIndex={0}
                role='button'
                aria-label={t('Edit Request Rule')}
                onClick={() => {
                  setEditing(profile)
                  setDrawerOpen(true)
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault()
                    setEditing(profile)
                    setDrawerOpen(true)
                  }
                }}
                className={`cursor-pointer space-y-2 rounded-xl border p-3 outline-none focus-visible:bg-muted ${
                  profile.enabled ? '' : 'opacity-60'
                }`}
              >
                <div className='flex items-start justify-between gap-2'>
                  <div className='min-w-0'>
                    <div className='truncate font-medium'>{profile.name}</div>
                    {profile.description ? (
                      <div className='text-muted-foreground mt-0.5 text-xs break-words'>
                        {profile.description}
                      </div>
                    ) : null}
                  </div>
                  <Switch
                    checked={profile.enabled}
                    onCheckedChange={() => toggleMutation.mutate(profile)}
                    onClick={(event) => event.stopPropagation()}
                    aria-label={t('Enabled')}
                  />
                </div>
                <p className='text-muted-foreground line-clamp-2 font-mono text-xs break-words'>
                  {profile.kind === 'header'
                    ? (profile.headers || '{}')
                    : profile.prompt}
                </p>
                <div className='flex flex-wrap items-center gap-1.5'>
                  {typeBadge(profile)}
                  {modeBadge(profile)}
                  <Badge variant='outline'>
                    {t('Channels')}{' '}
                    {Object.keys(profile.channel_models ?? {}).length}
                  </Badge>
                  <Badge variant='outline'>
                    {t('Models')}{' '}
                    {Object.values(profile.channel_models ?? {}).reduce(
                      (total, names) => total + names.length,
                      0
                    )}
                  </Badge>
                </div>
                <div className='flex items-center justify-end gap-1 border-t pt-1'>
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Delete')}
                    onClick={(event) => {
                      event.stopPropagation()
                      setDeleting(profile)
                    }}
                  >
                    <Trash2 className='size-4 text-destructive' />
                  </Button>
                </div>
              </div>
            ))
          )}
        </CardContent>
      </Card>

      <ProfileDrawer
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        profile={editing}
        onSaved={(saved) => {
          invalidate()
          setEditing(saved)
        }}
      />

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete jailbreak prompt?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('This also removes its channel and model bindings.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleting && deleteMutation.mutate(deleting.id)}
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}