export type RemoteState<T> =
  | { status: 'loading' }
  | { status: 'success'; value: T; loadedAt: string }
  | { status: 'error'; error: string }

export function remoteLoading<T>(): RemoteState<T> {
  return { status: 'loading' }
}

export function remoteSuccess<T>(value: T, loadedAt: string): RemoteState<T> {
  return { status: 'success', value, loadedAt }
}

export function remoteError<T>(error: string): RemoteState<T> {
  return { status: 'error', error }
}
