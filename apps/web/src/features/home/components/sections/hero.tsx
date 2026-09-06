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
import { Link } from '@tanstack/react-router'
import { ArrowRight, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { CheeseArt } from '../cheese-art'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

export function Hero(props: HeroProps) {
  const { t } = useTranslation()

  return (
    <section className='relative z-10 overflow-hidden px-6 pt-24 pb-16 md:pt-32 md:pb-24 lg:pt-36 lg:pb-28'>
      {/* Warm ambient wash — the palette's gold/crust pair. */}
      <div
        aria-hidden
        className='cheese-aurora pointer-events-none absolute inset-0 -z-10 opacity-30 dark:opacity-20'
        style={{
          background: [
            'radial-gradient(ellipse 60% 50% at 25% 20%, oklch(0.87 0.15 85 / 80%) 0%, transparent 70%)',
            'radial-gradient(ellipse 50% 40% at 75% 12%, oklch(0.83 0.16 65 / 55%) 0%, transparent 70%)',
            'radial-gradient(ellipse 40% 35% at 50% 90%, oklch(0.85 0.12 95 / 40%) 0%, transparent 70%)',
          ].join(', '),
        }}
      />
      {/* Dotted grid, masked to a soft ellipse. */}
      <div
        aria-hidden
        className='absolute inset-0 -z-10 bg-[radial-gradient(circle_at_center,var(--border)_1px,transparent_1px)] [mask-image:radial-gradient(ellipse_65%_55%_at_50%_35%,black_20%,transparent_100%)] bg-[size:2.25rem_2.25rem] opacity-[0.09]'
      />

      <div className='mx-auto grid max-w-6xl grid-cols-1 items-center gap-12 lg:grid-cols-12 lg:gap-8'>
        <div className='flex flex-col items-start text-left lg:col-span-6'>
          <div
            className='landing-animate-fade-up border-primary/30 bg-primary/10 text-primary mb-5 inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-[11px] font-medium shadow-xs'
            style={{ animationDelay: '0ms' }}
          >
            <Sparkles className='size-3.5' />
            <span>{t('home.hero.badge')}</span>
          </div>

          <h1
            className='landing-animate-fade-up text-[clamp(2.25rem,4.5vw,3.25rem)] leading-[1.15] font-bold tracking-tight'
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

          <div
            className='landing-animate-fade-up text-muted-foreground/60 mt-9 flex flex-wrap items-center gap-x-6 gap-y-2 text-xs opacity-0'
            style={{ animationDelay: '240ms' }}
          >
            <span className='flex items-center gap-1.5'>
              <span className='bg-primary size-1.5 rounded-full' />
              {t('home.hero.point.free')}
            </span>
            <span className='flex items-center gap-1.5'>
              <span className='bg-warning size-1.5 rounded-full' />
              {t('home.hero.point.checkin')}
            </span>
            <span className='flex items-center gap-1.5'>
              <span className='bg-chart-4 size-1.5 rounded-full' />
              {t('home.hero.point.roleplay')}
            </span>
          </div>
        </div>

        <div
          className='landing-animate-fade-up flex w-full justify-center opacity-0 lg:col-span-6'
          style={{ animationDelay: '320ms' }}
        >
          <CheeseArt className='mt-6 lg:mt-0' />
        </div>
      </div>
    </section>
  )
}