try {
  document.documentElement.classList.toggle(
    'dark',
    localStorage.getItem('excalibur-ui-theme') === 'dark',
  )
} catch {
  /* Optional preference. */
}
