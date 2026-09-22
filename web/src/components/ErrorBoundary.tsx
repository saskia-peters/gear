import React, { Component, type ReactNode } from 'react'
import styles from '../pages/PlaceholderPage.module.css'

interface ErrorBoundaryProps {
  children: ReactNode
}

interface ErrorBoundaryState {
  hasError: boolean
  error: Error | null
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error('Unhandled UI error:', error, errorInfo)
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className={styles.page}>
          <main className={styles.main}>
            <div className={styles.card} role="alert">
              <h1 className={styles.title}>Ein Fehler ist aufgetreten</h1>
              <p className={styles.description}>
                Die Anwendung konnte die Seite nicht laden. Bitte versuche es erneut.
              </p>
              <button
                type="button"
                className={styles.backLink}
                onClick={() => window.location.reload()}
              >
                Seite neu laden
              </button>
            </div>
          </main>
        </div>
      )
    }

    return this.props.children
  }
}
