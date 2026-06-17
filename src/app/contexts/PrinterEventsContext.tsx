import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, ReactNode } from 'react';
import { generateId } from '../lib/id';

export type PrinterEventLevel = 'success' | 'warning' | 'error' | 'info';

export interface PrinterEvent {
  id: string;
  level: PrinterEventLevel;
  title: string;
  description?: string;
  printerId?: string;
  printerName?: string;
  timestamp: number; // epoch ms
  read: boolean;
}

// Input for addEvent — id/timestamp/read are filled in by the context.
export type PrinterEventInput = Omit<PrinterEvent, 'id' | 'timestamp' | 'read'>;

interface PrinterEventsContextType {
  events: PrinterEvent[];
  unreadCount: number;
  addEvent: (event: PrinterEventInput) => void;
  markAllRead: () => void;
  clearAll: () => void;
}

const PrinterEventsContext = createContext<PrinterEventsContextType | undefined>(undefined);

const STORAGE_KEY = 'printfarm_printer_events';
// Keep the persisted history bounded so localStorage never grows without limit.
const MAX_EVENTS = 100;

function readStoredEvents(): PrinterEvent[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) {
      return [];
    }
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed
      .filter((value): value is PrinterEvent =>
        value &&
        typeof value.id === 'string' &&
        typeof value.title === 'string' &&
        typeof value.timestamp === 'number',
      )
      .slice(0, MAX_EVENTS);
  } catch {
    return [];
  }
}

export function PrinterEventsProvider({ children }: { children: ReactNode }) {
  const [events, setEvents] = useState<PrinterEvent[]>(() =>
    typeof window === 'undefined' ? [] : readStoredEvents(),
  );
  const eventsRef = useRef(events);
  eventsRef.current = events;

  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(events));
    } catch {
      // Ignore storage failures; events still work for the current session.
    }
  }, [events]);

  const addEvent = useCallback((event: PrinterEventInput) => {
    setEvents((previous) => {
      const next: PrinterEvent = {
        ...event,
        id: generateId(),
        timestamp: Date.now(),
        read: false,
      };
      return [next, ...previous].slice(0, MAX_EVENTS);
    });
  }, []);

  const markAllRead = useCallback(() => {
    setEvents((previous) =>
      previous.some((event) => !event.read)
        ? previous.map((event) => (event.read ? event : { ...event, read: true }))
        : previous,
    );
  }, []);

  const clearAll = useCallback(() => {
    setEvents([]);
  }, []);

  const unreadCount = useMemo(() => events.filter((event) => !event.read).length, [events]);

  const value = useMemo(
    () => ({ events, unreadCount, addEvent, markAllRead, clearAll }),
    [events, unreadCount, addEvent, markAllRead, clearAll],
  );

  return <PrinterEventsContext.Provider value={value}>{children}</PrinterEventsContext.Provider>;
}

export function usePrinterEvents() {
  const context = useContext(PrinterEventsContext);
  if (context === undefined) {
    throw new Error('usePrinterEvents must be used within a PrinterEventsProvider');
  }
  return context;
}
