import { Component, type ErrorInfo, type ReactNode } from "react";

/**
 * Stops one broken component from blanking the entire app.
 *
 * React unmounts the whole tree on an uncaught render error, which turns a
 * single undefined field into a white page with nothing to act on. This
 * catches it and says what happened, which at minimum leaves a reload button
 * and an error worth reporting.
 */
export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state: { error: Error | null } = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // The console is the only place this can go for now; it's what someone
    // will be asked for when reporting a blank screen.
    console.error("render error:", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;

    return (
      <div className="centered">
        <h2 style={{ marginBottom: 12 }}>Something broke on this page</h2>
        <p className="small muted" style={{ maxWidth: "44ch", margin: "0 auto 20px" }}>
          This is a bug on our side, not a problem with your account or your matches. Reloading usually works — the app
          updates in the background and can briefly be out of step with the server.
        </p>
        <button className="btn" onClick={() => window.location.reload()}>
          Reload
        </button>
        <p className="mono caption muted" style={{ marginTop: 20 }}>
          {this.state.error.message}
        </p>
      </div>
    );
  }
}
