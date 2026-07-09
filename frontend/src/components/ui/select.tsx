import { ChevronDown } from 'lucide-react'
import type { SelectHTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

type SelectProps = SelectHTMLAttributes<HTMLSelectElement>

export function Select({ className, children, ...props }: SelectProps) {
  return (
    <span className={cn('relative inline-flex h-10 items-center', className)}>
      <select
        className="h-full w-full appearance-none rounded-md border border-border bg-white py-0 pl-2 pr-6 text-sm font-semibold text-slate-900 outline-none transition hover:bg-slate-50 focus:border-primary focus:ring-2 focus:ring-teal-100 disabled:cursor-not-allowed disabled:opacity-50"
        {...props}
      >
        {children}
      </select>
      <ChevronDown className="pointer-events-none absolute right-1.5 top-1/2 -translate-y-1/2 text-muted" size={15} />
    </span>
  )
}
