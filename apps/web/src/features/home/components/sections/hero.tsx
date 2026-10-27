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
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useSystemConfig } from '@/hooks/use-system-config'
import { getHomeStats, type HomeStats } from '@/lib/api'
import { formatNumber, formatPercent } from '@/lib/format'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

export function Hero(props: HeroProps) {
  const { systemName } = useSystemConfig()
  const displayName = systemName || 'New API'
  const { t } = useTranslation()

  const { data: homeStats, isLoading, isError } = useQuery<HomeStats | undefined>({
    queryKey: ['home-stats'],
    queryFn: getHomeStats,
    staleTime: 10 * 60_000,
    retry: false,
  })

  const requests = homeStats?.requests ?? 0
  const successRate = homeStats?.success_rate ?? 0
  const failureRate = homeStats?.failure_rate ?? 0

  // 无流量日后端返回 requests=0 的合法全零响应，与加载/失败态同样保持占位符。
  const ready = !isLoading && !isError && requests > 0

  return (
    <section className='relative z-10 overflow-hidden px-6 pt-24 pb-16 md:pt-32 md:pb-24 lg:pt-36 lg:pb-28'>
      <div
        aria-hidden
        className='pointer-events-none absolute inset-0 -z-10 opacity-30 dark:opacity-20'
        style={{
          background: [
            'radial-gradient(ellipse 60% 50% at 25% 20%, oklch(0.72 0.16 275 / 45%) 0%, transparent 70%)',
            'radial-gradient(ellipse 50% 40% at 75% 12%, oklch(0.72 0.13 190 / 35%) 0%, transparent 70%)',
          ].join(', '),
        }}
      />
      {/* Dotted grid, masked to a soft ellipse. */}
      <div
        aria-hidden
        className='absolute inset-0 -z-10 bg-[radial-gradient(circle_at_center,var(--border)_1px,transparent_1px)] [mask-image:radial-gradient(ellipse_65%_55%_at_50%_35%,black_20%,transparent_100%)] bg-[size:2rem_2rem] opacity-[0.14] dark:opacity-[0.11]'
      />

      <div className='mx-auto grid max-w-6xl grid-cols-1 items-center gap-12 lg:grid-cols-12 lg:gap-8'>
        <div className='flex flex-col items-start text-left lg:col-span-6'>
          <div
            className='landing-animate-fade-up border-primary/30 bg-primary/10 text-primary [text-shadow:var(--hero-badge-text-shadow)] mb-5 inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-[11px] font-medium shadow-xs'
            style={{ animationDelay: '0ms' }}
          >
            <Sparkles className='size-3.5' />
            <span>{displayName} · {t('home.hero.badge')}</span>
          </div>

          <h1
            className='landing-animate-fade-up text-foreground/90 text-[clamp(2.25rem,4.5vw,3.25rem)] leading-[1.15] font-bold tracking-tight'
            style={{ animationDelay: '60ms' }}
          >
            {t('home.hero.titleLead')}
            <br />
            <span className='from-primary via-warning to-chart-4 bg-gradient-to-r bg-clip-text text-transparent'>
              {t('home.hero.titleAccent')}
            </span>
          </h1>

          <p
            className='landing-animate-fade-up text-muted-foreground/80 mt-5 max-w-xl text-base leading-relaxed opacity-0 md:text-[15px]'
            style={{ animationDelay: '120ms' }}
          >
            {t('home.hero.subtitle')}
          </p>

          <div
            className='landing-animate-fade-up mt-8 flex flex-wrap items-center gap-3 opacity-0'
            style={{ animationDelay: '180ms' }}
          >
            {props.isAuthenticated ? (
              <Button
                className='group h-11 rounded-lg px-5 text-sm font-medium'
                render={<Link to='/dashboard' />}
              >
                {t('Go to Dashboard')}
                <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
              </Button>
            ) : (
              <>
                <Button
                  className='group h-11 rounded-lg px-5 text-sm font-medium'
                  render={<Link to='/sign-up' />}
                >
                  {t('home.cta.join')}
                  <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
                </Button>
                <Button
                  variant='outline'
                  className='border-border/50 hover:border-border hover:bg-muted/50 h-11 rounded-lg px-5 text-sm font-medium'
                  render={<Link to='/pricing' />}
                >
                  {t('View Pricing')}
                </Button>
                <Button
                  variant='ghost'
                  className='text-muted-foreground hover:text-foreground h-11 rounded-lg px-4 text-sm font-medium'
                  render={<Link to='/sign-in' />}
                >
                  {t('Sign In')}
                </Button>
              </>
            )}
          </div>

        </div>

        <div
          className='landing-animate-fade-up flex w-full justify-center opacity-0 lg:col-span-6'
          style={{ animationDelay: '320ms' }}
        >
          <div className='border-border/60 bg-card-surface backdrop-blur-card w-full max-w-[420px] rounded-2xl border p-4 shadow-lg'>
            <div className='text-muted-foreground mb-3 flex items-center justify-between text-xs'>
              <span>{displayName} / {t('API Overview')}</span>
              <span className='text-success'>● {t('Running normally')}</span>
            </div>
            <div className='grid grid-cols-3 gap-2'>
              <div className='bg-muted/50 rounded-lg p-3'><span className='text-muted-foreground text-[10px]'>{t("Yesterday's requests")}</span><strong className='mt-1 block text-lg'>{ready ? formatNumber(requests) : '—'}</strong></div>
              <div className='bg-muted/50 rounded-lg p-3'><span className='text-muted-foreground text-[10px]'>{t('Success rate')}</span><strong className='text-success mt-1 block text-lg'>{ready ? formatPercent(successRate) : '—'}</strong></div>
              <div className='bg-muted/50 rounded-lg p-3'><span className='text-muted-foreground text-[10px]'>{t('Failure Rate')}</span><strong className='text-destructive mt-1 block text-lg'>{ready ? formatPercent(failureRate) : '—'}</strong></div>
            </div>
            <div className='bg-muted/35 mt-3 h-28 rounded-lg border p-3'>
              <div className='text-muted-foreground text-[10px]'>{t('From requests to cost, everything is clear')}</div>
              <div className='mt-8 flex items-end gap-1'>
                {[28, 42, 35, 58, 46, 72, 63, 80, 54, 68, 76, 88].map((height, index) => <span key={index} className='bg-primary/60 block flex-1 rounded-t' style={{ height: `${height}%` }} />)}
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
