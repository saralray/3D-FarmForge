export function roundToMaxTwoDecimals(value: number) {
  return Math.round(value * 100) / 100;
}

export function normalizeMaxTwoDecimals(value: unknown, fallback = 0) {
  return typeof value === 'number' && Number.isFinite(value)
    ? roundToMaxTwoDecimals(value)
    : fallback;
}

export function formatMaxTwoDecimals(value: number) {
  return new Intl.NumberFormat('en-US', {
    maximumFractionDigits: 2,
  }).format(roundToMaxTwoDecimals(value));
}
