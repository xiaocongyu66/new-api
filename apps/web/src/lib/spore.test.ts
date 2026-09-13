import { describe, expect, it } from 'bun:test'
import {
  SPORE_LABEL,
  SPORE_UNITS_PER_SPORE,
  formatSpore,
  parseSporeToUnits,
  sporeUnitsToValue,
} from './spore'

describe('spore currency helpers', () => {
  it('defines 10 units per spore', () => {
    expect(SPORE_UNITS_PER_SPORE).toBe(10)
    expect(SPORE_LABEL).toBe('菌种')
  })

  it('formats internal integer units to 1 decimal place string', () => {
    expect(formatSpore(0)).toBe('0.0')
    expect(formatSpore(1)).toBe('0.1')
    expect(formatSpore(10)).toBe('1.0')
    expect(formatSpore(25)).toBe('2.5')
    expect(formatSpore(123)).toBe('12.3')
    expect(formatSpore(-15)).toBe('-1.5')
    expect(formatSpore(null)).toBe('0.0')
    expect(formatSpore(undefined)).toBe('0.0')
  })

  it('parses user input string or number to internal integer units without float errors', () => {
    expect(parseSporeToUnits('0')).toBe(0)
    expect(parseSporeToUnits('0.1')).toBe(1)
    expect(parseSporeToUnits('0.7')).toBe(7) // 0.7 * 10 is 6.999999999999999 in JS float, round handles it
    expect(parseSporeToUnits('2.5')).toBe(25)
    expect(parseSporeToUnits(2.5)).toBe(25)
    expect(parseSporeToUnits('invalid')).toBe(0)
  })

  it('converts units to decimal value for form input fields', () => {
    expect(sporeUnitsToValue(0)).toBe(0)
    expect(sporeUnitsToValue(15)).toBe(1.5)
    expect(sporeUnitsToValue(20)).toBe(2)
    expect(sporeUnitsToValue(null)).toBe(0)
  })
})
