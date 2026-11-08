const relativeFormat = typeof Intl !== 'undefined' && 'RelativeTimeFormat' in Intl ? new Intl.RelativeTimeFormat(undefined, {numeric: 'auto'}) : null;

export function relativeTime(ms: number, now = Date.now()): string {
    const seconds = Math.round((ms - now) / 1000);
    const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [['year', 31536000], ['month', 2592000], ['day', 86400], ['hour', 3600], ['minute', 60]];
    if (!relativeFormat) {
        return new Date(ms).toLocaleString();
    }
    for (const [unit, size] of units) {
        if (Math.abs(seconds) >= size) {
            return relativeFormat.format(Math.round(seconds / size), unit);
        }
    }
    return 'just now';
}
