import { Bell, CheckCheck, Trash2, CheckCircle2, AlertTriangle, XCircle, Info } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from './ui/popover';
import { ScrollArea } from './ui/scroll-area';
import { usePrinterEvents, PrinterEvent, PrinterEventLevel } from '../contexts/PrinterEventsContext';

const LEVEL_ICON: Record<PrinterEventLevel, typeof Info> = {
  success: CheckCircle2,
  warning: AlertTriangle,
  error: XCircle,
  info: Info,
};

const LEVEL_COLOR: Record<PrinterEventLevel, string> = {
  success: 'text-green-600 dark:text-green-400',
  warning: 'text-amber-600 dark:text-amber-400',
  error: 'text-red-600 dark:text-red-400',
  info: 'text-blue-600 dark:text-blue-400',
};

function formatRelativeTime(timestamp: number): string {
  const diffSeconds = Math.round((Date.now() - timestamp) / 1000);
  if (diffSeconds < 60) {
    return 'just now';
  }
  const diffMinutes = Math.round(diffSeconds / 60);
  if (diffMinutes < 60) {
    return `${diffMinutes}m ago`;
  }
  const diffHours = Math.round(diffMinutes / 60);
  if (diffHours < 24) {
    return `${diffHours}h ago`;
  }
  const diffDays = Math.round(diffHours / 24);
  if (diffDays < 7) {
    return `${diffDays}d ago`;
  }
  return new Date(timestamp).toLocaleDateString();
}

function EventRow({ event }: { event: PrinterEvent }) {
  const Icon = LEVEL_ICON[event.level];
  return (
    <div
      className={`flex gap-3 px-4 py-3 ${
        event.read ? '' : 'bg-blue-50/60 dark:bg-blue-900/20'
      }`}
    >
      <Icon className={`mt-0.5 size-4 shrink-0 ${LEVEL_COLOR[event.level]}`} />
      <div className="min-w-0 flex-1">
        <div className="flex items-start justify-between gap-2">
          <p className="truncate text-sm font-medium text-gray-900 dark:text-gray-100">
            {event.title}
          </p>
          <span className="shrink-0 whitespace-nowrap text-xs text-gray-400 dark:text-gray-500">
            {formatRelativeTime(event.timestamp)}
          </span>
        </div>
        {event.description && (
          <p className="mt-0.5 break-words text-xs text-gray-500 dark:text-gray-400">
            {event.description}
          </p>
        )}
      </div>
    </div>
  );
}

/**
 * Notification center. Persists the printer status events (started / completed /
 * stopped / paused / error, plus new queue submissions) that
 * `PrinterStatusNotifier` also flashes as ephemeral toasts. Rendered inline next
 * to the theme toggle; shows a red dot while there are unread events.
 */
export function NotificationBell() {
  const { events, unreadCount, markAllRead, clearAll } = usePrinterEvents();

  return (
    <Popover
      onOpenChange={(open) => {
        if (open) {
          markAllRead();
        }
      }}
    >
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={`Printer notifications${unreadCount > 0 ? ` (${unreadCount} unread)` : ''}`}
          className="relative inline-flex size-9 items-center justify-center rounded-md text-gray-700 transition-colors hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-gray-800"
        >
          <Bell className="size-5" />
          {unreadCount > 0 && (
            <span className="absolute right-1.5 top-1.5 size-2 rounded-full bg-red-500 ring-2 ring-white dark:ring-gray-900" />
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent side="top" align="start" sideOffset={8} className="w-80 p-0">
        <div className="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700">
          <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100">Printer history</h3>
          {events.length > 0 && (
            <div className="flex items-center gap-1">
              <button
                type="button"
                onClick={markAllRead}
                title="Mark all as read"
                className="flex items-center gap-1 rounded px-2 py-1 text-xs text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-200"
              >
                <CheckCheck className="size-3.5" />
              </button>
              <button
                type="button"
                onClick={clearAll}
                title="Clear all"
                className="flex items-center gap-1 rounded px-2 py-1 text-xs text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-200"
              >
                <Trash2 className="size-3.5" />
              </button>
            </div>
          )}
        </div>

        {events.length === 0 ? (
          <div className="px-4 py-10 text-center text-sm text-gray-500 dark:text-gray-400">
            No printer activity yet.
          </div>
        ) : (
          <ScrollArea className="max-h-96">
            <div className="divide-y divide-gray-100 dark:divide-gray-800">
              {events.map((event) => (
                <EventRow key={event.id} event={event} />
              ))}
            </div>
          </ScrollArea>
        )}
      </PopoverContent>
    </Popover>
  );
}
