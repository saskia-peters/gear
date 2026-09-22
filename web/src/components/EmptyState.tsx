import styles from './EmptyState.module.css'

interface EmptyStateProps {
  message?: string
  description?: string
}

export function EmptyState({
  message = 'Keine Werkzeuge vorhanden',
  description = 'Aktuell sind keine Werkzeuge in dieser Ansicht vorhanden.',
}: EmptyStateProps) {
  return (
    <div className={styles.card} role="status">
      <div className={styles.iconWrapper} aria-hidden="true">
        <svg
          className={styles.icon}
          viewBox="0 0 24 24"
          xmlns="http://www.w3.org/2000/svg"
        >
          <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
        </svg>
      </div>
      <h2 className={styles.title}>{message}</h2>
      {description && <p className={styles.description}>{description}</p>}
    </div>
  )
}
