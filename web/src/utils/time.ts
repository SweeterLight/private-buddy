import i18n from '../i18n';

/**
 * Format a message timestamp for display.
 *
 * - Same day: time only (e.g. "14:30:00")
 * - Yesterday: "Yesterday" + time (e.g. "Yesterday 14:30:00")
 * - Older: full date + time (e.g. "2025-04-30 14:30:00")
 */
export function formatMessageTime(date: Date): string {
  const now = new Date();
  const timeStr = date.toLocaleTimeString();

  const isSameDay =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate();

  if (isSameDay) {
    return timeStr;
  }

  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  const isYesterday =
    date.getFullYear() === yesterday.getFullYear() &&
    date.getMonth() === yesterday.getMonth() &&
    date.getDate() === yesterday.getDate();

  if (isYesterday) {
    const yesterdayLabel = i18n.t('chat.yesterday');
    return `${yesterdayLabel} ${timeStr}`;
  }

  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day} ${timeStr}`;
}
