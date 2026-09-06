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
import { Ban, Code2, Drama, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

/**
 * Usage policy section.
 *
 * The site's stance is a hard rule, not a suggestion: no code generation.
 * The layout leads with what IS welcome (roleplay / creative writing) so
 * the prohibition reads as a scope definition rather than pure scolding,
 * then states the ban unambiguously in its own bordered callout.
 */
export function UsagePolicy() {
  const { t } = useTranslation()

  const welcome = [
    t('home.policy.welcome.item.roleplay'),
    t('home.policy.welcome.item.story'),
    t('home.policy.welcome.item.chat'),
    t('home.policy.welcome.item.translate'),
  ]

  return (
    <section className='relative z-10 px-6 py-16 md:py-20'>
      <div className='mx-auto max-w-6xl'>
        <div className='mb-10 text-center'>
          <h2 className='text-2xl font-bold tracking-tight sm:text-3xl'>
            {t('home.policy.title')}
          </h2>
          <p className='text-muted-foreground/75 mx-auto mt-3 max-w-xl text-sm leading-relaxed'>
            {t('home.policy.subtitle')}
          </p>
        </div>

        <div className='grid gap-4 md:grid-cols-2'>
          {/* Welcome — roleplay and creative use. */}
          <div className='border-border/60 bg-background/60 flex flex-col gap-4 rounded-xl border p-6'>
            <div className='flex items-center gap-3'>
              <span className='border-primary/25 bg-primary/10 text-primary inline-flex size-10 items-center justify-center rounded-lg border'>
                <Drama className='size-5' />
              </span>
              <h3 className='text-base font-semibold'>
                {t('home.policy.welcome.title')}
              </h3>
            </div>
            <p className='text-muted-foreground/80 text-sm leading-relaxed'>
              {t('home.policy.welcome.body')}
            </p>
            <ul className='mt-1 grid gap-2 sm:grid-cols-2'>
              {welcome.map((item) => (
                <li
                  key={item}
                  className='text-muted-foreground flex items-start gap-2 text-[13px] leading-relaxed'
                >
                  <span className='bg-primary mt-1.5 size-1.5 shrink-0 rounded-full' />
                  {item}
                </li>
              ))}
            </ul>
          </div>

          {/* Prohibited — code generation. Stated as a rule. */}
          <div className='border-destructive/30 bg-destructive/5 flex flex-col gap-4 rounded-xl border p-6'>
            <div className='flex items-center gap-3'>
              <span className='border-destructive/25 bg-destructive/10 text-destructive inline-flex size-10 items-center justify-center rounded-lg border'>
                <Ban className='size-5' />
              </span>
              <h3 className='text-base font-semibold'>
                {t('home.policy.forbidden.title')}
              </h3>
            </div>
            <p className='text-muted-foreground/80 text-sm leading-relaxed'>
              {t('home.policy.forbidden.body')}
            </p>
            <div className='border-destructive/25 bg-background/60 flex items-start gap-2.5 rounded-lg border p-3'>
              <Code2 className='text-destructive mt-0.5 size-4 shrink-0' />
              <p className='text-[13px] leading-relaxed'>
                {t('home.policy.forbidden.rule')}
              </p>
            </div>
            <div className='text-muted-foreground/70 flex items-start gap-2 text-xs leading-relaxed'>
              <TriangleAlert className='mt-0.5 size-3.5 shrink-0' />
              {t('home.policy.forbidden.consequence')}
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}