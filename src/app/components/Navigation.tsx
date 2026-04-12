import { Link, useLocation } from 'react-router';
import { AnimatePresence, motion } from 'motion/react';
import { LayoutDashboard, List, BarChart3, LogOut, Settings, ClipboardList, ExternalLink } from 'lucide-react';
import { ThemeToggle } from './ThemeToggle';
import { useAuth } from '../contexts/AuthContext';
import { Button } from './ui/button';
import { GOOGLE_FORM_URL, PUBLIC_VIEWER_MODE } from '../lib/runtimeConfig';
import { useSidebar } from '../contexts/SidebarContext';
import stemlabLogo from '../../../CUD-STEM-LAB-logoBBGv2.svg';

export function Navigation() {
  const location = useLocation();
  const { user, logout } = useAuth();
  const { isCollapsed, toggleSidebar } = useSidebar();

  const navItems = [
    { path: '/', label: 'Dashboard', icon: LayoutDashboard },
    { path: '/queue', label: 'Queue', icon: List },
    { path: '/analytics', label: 'Analytics', icon: BarChart3 },
  ];
  const adminNavItems = !PUBLIC_VIEWER_MODE && user?.role === 'admin'
    ? [{ path: '/settings', label: 'Settings', icon: Settings }]
    : [];

  const isActive = (path: string) => {
    if (path === '/') {
      return location.pathname === '/';
    }
    return location.pathname.startsWith(path);
  };

  return (
    <motion.nav
      animate={{ width: isCollapsed ? 84 : 288 }}
      transition={{ duration: 0.3, ease: 'easeInOut' }}
      className="relative flex h-screen flex-shrink-0 flex-col border-r border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-900"
    >
      <div className="p-6 border-b border-gray-200 dark:border-gray-700">
        <div className="space-y-3 overflow-hidden">
          <img
            src={stemlabLogo}
            alt="CUD Stemlab PrintFarm logo"
            className={`w-auto max-w-full dark:invert dark:brightness-200 ${isCollapsed ? 'h-9' : 'h-14'}`}
          />
          <AnimatePresence initial={false}>
            {!isCollapsed && (
              <motion.p
                initial={{ opacity: 0, y: -4 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -4 }}
                className="text-xs text-gray-500 dark:text-gray-400"
              >
                Manager v1.0
              </motion.p>
            )}
          </AnimatePresence>
        </div>
      </div>

      <div className="flex-1 p-4">
        <div className="space-y-1">
          {[...navItems, ...adminNavItems].map((item) => (
            <Link
              key={item.path}
              to={item.path}
              className={`flex items-center rounded-lg px-4 py-3 transition-colors ${
                isActive(item.path)
                  ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 font-medium'
                  : 'text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800'
              }`}
            >
              <item.icon className="size-5" />
              <AnimatePresence initial={false}>
                {!isCollapsed && (
                  <motion.span
                    initial={{ opacity: 0, x: -8 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: -8 }}
                    className="ml-3 whitespace-nowrap"
                  >
                    {item.label}
                  </motion.span>
                )}
              </AnimatePresence>
            </Link>
          ))}
          <button
            type="button"
            onClick={() => window.open(GOOGLE_FORM_URL, '_blank', 'noopener,noreferrer')}
            className="flex w-full items-center rounded-lg px-4 py-3 text-gray-700 transition-colors hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800"
          >
            <ClipboardList className="size-5" />
            <AnimatePresence initial={false}>
              {!isCollapsed && (
                <motion.div
                  initial={{ opacity: 0, x: -8 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, x: -8 }}
                  className="ml-3 flex min-w-0 flex-1 items-center justify-between gap-2"
                >
                  <span className="whitespace-nowrap">ฟอร์มขอพิมพ์งาน</span>
                  <ExternalLink className="size-4 shrink-0 text-gray-400 dark:text-gray-500" />
                </motion.div>
              )}
            </AnimatePresence>
          </button>
        </div>
      </div>

      <div className="p-4 border-t border-gray-200 dark:border-gray-700 space-y-3">
        {user && (
          <div className={`flex items-center rounded-lg bg-gray-50 p-3 dark:bg-gray-800 ${isCollapsed ? 'justify-center' : 'gap-3'}`}>
            <div className="flex size-10 items-center justify-center rounded-full bg-blue-500 font-semibold text-white">
              {user.name.charAt(0)}
            </div>
            <AnimatePresence initial={false}>
              {!isCollapsed && (
                <motion.div
                  initial={{ opacity: 0, x: -8 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, x: -8 }}
                  className="min-w-0 flex-1"
                >
                  <div className="truncate text-sm font-medium dark:text-white">
                    {user.name}
                  </div>
                  <div className="text-xs capitalize text-gray-500 dark:text-gray-400">
                    {user.role}
                  </div>
                </motion.div>
              )}
            </AnimatePresence>
          </div>
        )}
        
        <div className={`flex items-center ${isCollapsed ? 'justify-center' : 'justify-between'}`}>
          {!isCollapsed && <span className="text-sm text-gray-600 dark:text-gray-400">Theme</span>}
          <ThemeToggle />
        </div>

        {user && !PUBLIC_VIEWER_MODE && user.role !== 'viewer' && (
          <Button
            variant="outline"
            className={isCollapsed ? 'w-full px-0' : 'w-full'}
            onClick={logout}
          >
            <LogOut className="size-4 mr-2" />
            {!isCollapsed && 'Logout'}
          </Button>
        )}

        <AnimatePresence initial={false}>
          {!isCollapsed && (
            <motion.div
              initial={{ opacity: 0, y: 4 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: 4 }}
              className="space-y-1 text-xs text-gray-500 dark:text-gray-400"
            >
              <div>{PUBLIC_VIEWER_MODE ? 'Access' : 'Developer'}</div>
              {PUBLIC_VIEWER_MODE ? (
                <div className="truncate">Public Viewer Mode</div>
              ) : (
                <>
                  <div className="truncate">Saral Assabumrungrat CUD61</div>
                  <div className="truncate">Thiraphat Srichit CUD62</div>
                </>
              )}
            </motion.div>
          )}
        </AnimatePresence>
      </div>
    </motion.nav>
  );
}
