import { Outlet } from 'react-router';
import { motion } from 'motion/react';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { Navigation } from '../components/Navigation';
import { useSidebar } from '../contexts/SidebarContext';

export function Root() {
  const { isCollapsed, toggleSidebar } = useSidebar();

  return (
    <div className="relative flex h-screen bg-gray-50 dark:bg-gray-950">
      <Navigation />
      <motion.button
        type="button"
        onClick={toggleSidebar}
        aria-label={isCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        animate={{ left: isCollapsed ? 72 : 276 }}
        transition={{ duration: 0.3, ease: 'easeInOut' }}
        className="absolute top-6 z-20 flex size-7 items-center justify-center rounded-full border border-gray-200 bg-white shadow-md transition-colors hover:bg-gray-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
      >
        {isCollapsed ? <ChevronRight className="size-4" /> : <ChevronLeft className="size-4" />}
      </motion.button>
      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
