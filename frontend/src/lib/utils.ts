import { clsx, type ClassValue } from 'clsx'

export function cn(...inputs: ClassValue[]) {
  return clsx(inputs)
}

export function formatTime(seconds: number) {
  if (!Number.isFinite(seconds)) {
    return '00:00'
  }
  const minutes = Math.floor(seconds / 60)
  const rest = Math.floor(seconds % 60)
  const decimal = Math.floor((seconds % 1) * 10)
  return `${String(minutes).padStart(2, '0')}:${String(rest).padStart(2, '0')}.${decimal}`
}
