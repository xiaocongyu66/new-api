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
import { CalendarCheck, HandHeart, Layers, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'

interface FeaturesProps {
  className?: string
}

export function Features(_props: FeaturesProps) {
  const { t } = useTranslation()

  const items = [
    {
      key: 'free',
      icon: HandHeart,
      title: t('home.feature.free.title'),
      desc: t('home.feature.free.desc'),
      tone: 'text-primary border-primary/25 bg-primary/10',
    },
    {
      key: 'checkin',
      icon: CalendarCheck,
      title: t('home.feature.checkin.title'),
      desc: t('home.feature.checkin.desc'),
      tone: 'text-warning border-warning/25 bg-warning/10',
    },
    {
      key: 'models',
      icon: Layers,
      title: t('home.feature.models.title'),
      desc: t('home.feature.models.desc'),
      tone: 'text-chart-4 border-chart-4/25 bg-chart-4/10',
    },
    {
      key: 'stable',
      icon: ShieldCheck,
      title: t('home.feature.stable.title'),
      desc: t('home.feature.stable.desc'),
      tone: 'text-success border-success/25 bg-success/10',
    },
  ]

  return (
    <section className='relative z-10 px-6 py-16 md:py-20'>
      <div className='mx-auto max-w-6xl'>
        <div className='mb-10 text-center'>
          <h2 className='text-2xl font-bold tracking-tight sm:text-3xl'>
            {t('home.features.title')}
          </h2>
          <p className='text-muted-foreground/75 mx-auto mt-3 max-w-xl text-sm leading-relaxed'>
            {t('home.features.subtitle')}
          </p>
        </div>

        <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
          {items.map((item) => {
            const Icon = item.icon
            return (
              <div
                key={item.key}
                className='group border-border/60 bg-background/60 hover:border-primary/40 flex flex-col gap-3 rounded-xl border p-5 transition-all duration-300 hover:-translate-y-0.5 hover:shadow-md'
              >
                <div
                  className={`inline-flex size-10 items-center justify-center rounded-lg border ${item.tone}`}
                >
                  <Icon className='size-5' />
                </div>
                <div>
                  <h3 className='text-sm font-semibold'>{item.title}</h3>
                  <p className='text-muted-foreground/75 mt-1.5 text-xs leading-relaxed'>
                    {item.desc}
                  </p>
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </section>
  )
}