/**
 * UX convenience for the local sample preview. Not a security boundary.
 * Production builds leave VITE_READ_ONLY_PREVIEW unset/false.
 */
export const READ_ONLY_PREVIEW = import.meta.env.VITE_READ_ONLY_PREVIEW === 'true'
