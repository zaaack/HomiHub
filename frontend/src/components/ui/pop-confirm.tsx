import * as React from 'react'
import * as Popover from '@radix-ui/react-popover'
import { useTranslation } from 'react-i18next'

interface PopConfirmProps {
  children: React.ReactNode
  onConfirm: () => void | Promise<void>
  title?: string
  description?: string
  confirmText?: string
  cancelText?: string
  disabled?: boolean
}

export function PopConfirm({
  children,
  onConfirm,
  title,
  description,
  confirmText,
  cancelText,
  disabled,
}: PopConfirmProps) {
  const { t } = useTranslation()
  const [open, setOpen] = React.useState(false)

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild disabled={disabled}>
        {children}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          className="z-[9999] w-64 rounded-xl border border-[var(--app-border)] bg-[var(--app-card)] p-3 shadow-xl"
          sideOffset={4}
          align="start"
        >
          {title && (
            <div className="mb-1 text-sm font-semibold">{title}</div>
          )}
          {description && (
            <p className="mb-3 text-xs text-[var(--app-muted)]">{description}</p>
          )}
          <div className="flex gap-2">
            <button
              onClick={() => setOpen(false)}
              className="flex-1 rounded-lg border border-[var(--app-border)] px-3 py-1.5 text-xs font-medium hover:bg-[var(--app-card-sub)]"
            >
              {cancelText ?? t('common.cancel')}
            </button>
            <button
              onClick={() => {
                const result = onConfirm()
                if (result && typeof (result as Promise<void>).then === 'function') {
                  (result as Promise<void>).then(() => setOpen(false))
                } else {
                  setOpen(false)
                }
              }}
              className="flex-1 rounded-lg bg-[var(--app-danger)] px-3 py-1.5 text-xs font-medium text-white hover:bg-[var(--app-danger)]/90"
            >
              {confirmText ?? t('common.confirm')}
            </button>
          </div>
          <Popover.Arrow className="fill-[var(--app-card)]" />
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  )
}
