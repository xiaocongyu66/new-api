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
import { cn } from '@/lib/utils'

import { useCountUpText } from '@/hooks/use-count-up-text'

/**
 * Renders a formatted number that counts up to its final value.
 *
 * The final string is rendered invisibly underneath and the live value is
 * overlaid on top, so the element always reserves the width of its finished
 * text — a growing number never pushes the unit or neighbouring cells
 * sideways. `active` gates the animation (e.g. off-screen cards).
 */
export function CountUp(props: {
  text: string
  active?: boolean
  className?: string
}) {
  const value = useCountUpText(props.text, props.active ?? true)

  return (
    <span
      className={cn('relative inline-block whitespace-nowrap', props.className)}
      aria-label={props.text}
    >
      <span className='invisible'>{props.text}</span>
      <span className='absolute inset-0 whitespace-nowrap' aria-hidden='true'>
        {value}
      </span>
    </span>
  )
}
