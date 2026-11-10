import { Component, type ReactNode } from 'react';

interface State {
  error: Error | null;
}

/**
 * Shows what went wrong instead of a blank page when rendering fails, so
 * the error can be reported.
 */
export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    return (
      <div className="crash" role="alert">
        <h1 className="empty-title">Stim stoppet på en feil</h1>
        <p>Last siden på nytt for å fortsette. Klientene og torrentene dine er ikke påvirket.</p>
        <pre className="crash-detail">{error.stack || error.message}</pre>
        <button type="button" className="btn btn-primary" onClick={() => window.location.reload()}>
          Last siden på nytt
        </button>
      </div>
    );
  }
}
