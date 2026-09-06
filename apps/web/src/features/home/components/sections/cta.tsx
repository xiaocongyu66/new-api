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
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

interface CTAProps {
  className?: string
  isAuthenticated?: boolean
}

export function CTA(props: CTAProps) {
  const { t } = useTranslation()

  return (
    <section className='relative z-10 px-6 py-16 md:py-24'>
      <div className='mx-auto max-w-4xl'>
        <div className='border-primary/30 from-primary/15 via-warning/10 to-chart-4/15 relative overflow-hidden rounded-2xl border bg-gradient-to-br px-6 py-14 text-center sm:px-12'>
          <div
            aria-hidden
            className='pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_center,var(--border)_1.5px,transparent_1.5px)] bg-[size:1.75rem_1.75rem] opacity-30'
          />
          <div
            aria-hidden
            className='cheese-aurora pointer-events-none absolute inset-0 opacity-70 blur-2xl dark:opacity-40'
            style={{
              background: [
                'radial-gradient(ellipse 45% 55% at 30% 30%, oklch(0.88 0.14 85 / 50%) 0%, transparent 70%)',
                'radial-gradient(ellipse 40% 45% at 75% 70%, oklch(0.82 0.15 40 / 38%) 0%, transparent 70%)',
              ].join(', '),
            }}
          />

          <h2 className='relative text-2xl font-bold tracking-tight sm:text-3xl'>
            {t('home.cta.title')}
          </h2>
          <p className='text-muted-foreground/80 relative mx-auto mt-3 max-w-lg text-sm leading-relaxed'>
            {t('home.cta.subtitle')}
          </p>

          <div className='relative mt-8 flex flex-wrap items-center justify-center gap-3'>
            {props.isAuthenticated ? (
              <Button
                className='group h-11 rounded-lg px-6 text-sm font-medium'
                render={<Link to='/dashboard' />}
              >
                {t('Go to Dashboard')}
                <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
              </Button>
            ) : (
              <>
                <Button
                  className='group h-11 rounded-lg px-6 text-sm font-medium'
                  render={<Link to='/sign-up' />}
                >
                  {t('home.cta.join')}
                  <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
                </Button>
                <Button
                  variant='outline'
                  className='border-border/50 hover:border-border hover:bg-muted/50 h-11 rounded-lg px-6 text-sm font-medium'
                  render={<Link to='/sign-in' />}
                >
                  {t('Sign In')}
                </Button>
              </>
            )}
          </div>
        </div>
      </div>
    </section>
  )
}