export type ScrollbarVisibility = 'hover' | 'always' | 'never'

export type ScrollbarConfig = {
  /** `hover` preserves the compact default; use `always` or `never` as needed. */
  visible: ScrollbarVisibility
}

export const scrollbarConfig: ScrollbarConfig = {
  visible: 'hover',
}
