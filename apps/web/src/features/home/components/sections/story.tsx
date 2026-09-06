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
import { CalendarCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'

/**
 * "Our story" — the site went live on 2026-08-17, so the honest angle is
 * that we are young. Framed as a promise rather than an apology.
 */
export function Story() {
  const { t } = useTranslation()

  const tags = [
    t('home.story.tag.free'),
    t('home.story.tag.young'),
    t('home.story.tag.growing'),
  ]

  return (
    <section className='relative z-10 overflow-hidden px-6 py-16 md:py-20'>
      <div className='mx-auto max-w-3xl'>
        <div className='border-border/60 from-primary/5 to-chart-4/5 relative rounded-2xl border bg-gradient-to-br px-6 py-10 sm:px-12 sm:py-14'>
          {/* Floating date medallion */}
          <div
            aria-hidden
            className='bg-background border-primary/30 text-primary pointer-events-none absolute -top-5 left-8 flex size-11 items-center justify-center rounded-full border shadow-xs'
          >
            <CalendarCheck className='size-5' />
          </div>

          <div className='text-primary mb-4 flex items-center gap-2 text-xs font-semibold tracking-[0.15em] uppercase'>
            <span className='bg-primary size-1.5 rounded-full' />
            {t('home.story.eyebrow')}
          </div>

          <h2 className='text-2xl font-bold tracking-tight sm:text-3xl'>
            {t('home.story.title')}
          </h2>

          <div className='text-muted-foreground/85 mt-5 space-y-4 text-[15px] leading-relaxed'>
            <p>{t('home.story.p1')}</p>
            <p>{t('home.story.p2')}</p>
            <p>{t('home.story.p3')}</p>
          </div>

          <div className='mt-8 flex flex-wrap gap-2'>
            {tags.map((tag) => (
              <span
                key={tag}
                className='border-border/60 bg-background/70 text-muted-foreground rounded-full border px-3 py-1 text-xs font-medium'
              >
                {tag}
              </span>
            ))}
          </div>
        </div>
      </div>
    </section>
  )
}