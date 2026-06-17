import { RouterProvider } from 'react-router';
import { router } from './routes.tsx';
import { ThemeProvider } from './components/ThemeProvider';
import { BrandingApplier } from './components/BrandingApplier';
import { AuthProvider } from './contexts/AuthContext';
import { SidebarProvider } from './contexts/SidebarContext';
import { Toaster } from './components/ui/sonner';

export default function App() {
  return (
    <ThemeProvider>
      <BrandingApplier />
      <AuthProvider>
        <SidebarProvider>
          <RouterProvider router={router} />
          <Toaster position="bottom-right" />
        </SidebarProvider>
      </AuthProvider>
    </ThemeProvider>
  );
}
