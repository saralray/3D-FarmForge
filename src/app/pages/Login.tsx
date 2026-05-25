import { useState } from 'react';
import { useNavigate, useLocation, Navigate } from 'react-router';
import { useAuth } from '../contexts/AuthContext';
import { Card } from '../components/ui/card';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { Alert } from '../components/ui/alert';
import { Eye, EyeOff, ClipboardList } from 'lucide-react';
import { PUBLIC_VIEWER_MODE } from '../lib/runtimeConfig';
import { useIntegrationSettings } from '../lib/settingsApi';
import stemlabLogo from '../../../CUD-STEM-LAB-logoBBGv2.svg';

export function Login() {
  if (PUBLIC_VIEWER_MODE) {
    return <Navigate to="/" replace />;
  }

  const navigate = useNavigate();
  const location = useLocation();
  const { login, loginAsViewer } = useAuth();
  const { googleFormUrl } = useIntegrationSettings();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const isAdminPage = location.pathname === '/admin';

  const from = (location.state as any)?.from?.pathname || '/';

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setIsLoading(true);

    try {
      const result = await login(username, password);
      if (result.success) {
        navigate(from, { replace: true });
      } else {
        setError(result.error ?? 'Unable to sign in.');
      }
    } catch {
      setError('An error occurred. Please try again.');
    } finally {
      setIsLoading(false);
    }
  };

  const handleViewerLogin = async () => {
    setError('');
    setIsLoading(true);

    try {
      const result = await loginAsViewer();
      if (result.success) {
        navigate(from, { replace: true });
      } else {
        setError(result.error ?? 'Unable to continue as viewer.');
      }
    } catch {
      setError('An error occurred. Please try again.');
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-100 via-white to-sky-100 dark:from-gray-950 dark:via-gray-900 dark:to-gray-950 p-4">
      <div className="w-full max-w-lg space-y-6">
        <div className="text-center">
          <div className="flex items-center justify-center gap-2 mb-4">
            <img
              src={stemlabLogo}
              alt="CUD Stemlab PrintFarm logo"
              className="h-24 w-auto max-w-full dark:invert dark:brightness-200"
            />
          </div>
          <p className="text-gray-600 dark:text-gray-400 mt-2">
            {isAdminPage ? 'Admin sign in' : 'Choose how to enter the print farm system'}
          </p>
        </div>

        <Card className="p-6 dark:bg-gray-800 dark:border-gray-700">
          <div className="space-y-4">
            {isAdminPage ? (
              <form onSubmit={handleSubmit} className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="username">Username</Label>
                  <Input
                    id="username"
                    type="text"
                    placeholder="Enter your username"
                    value={username}
                    onChange={(e) => setUsername(e.target.value.trimStart())}
                    required
                    autoComplete="username"
                    spellCheck={false}
                    autoCapitalize="none"
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="password">Password</Label>
                  <div className="relative">
                    <Input
                      id="password"
                      type={showPassword ? 'text' : 'password'}
                      placeholder="Enter your password"
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                      required
                      autoComplete="current-password"
                    />
                    <button
                      type="button"
                      onClick={() => setShowPassword(!showPassword)}
                      aria-label={showPassword ? 'Hide password' : 'Show password'}
                      className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-700 dark:hover:text-gray-300"
                    >
                      {showPassword ? (
                        <EyeOff className="size-4" />
                      ) : (
                        <Eye className="size-4" />
                      )}
                    </button>
                  </div>
                </div>

                {error && (
                  <Alert variant="destructive" className="py-2">
                    {error}
                  </Alert>
                )}

                <Button type="submit" className="w-full" disabled={isLoading}>
                  {isLoading ? 'Signing in...' : 'Login as Admin'}
                </Button>
              </form>
            ) : (
              <div className="space-y-4">
                {error && (
                  <Alert variant="destructive" className="py-2">
                    {error}
                  </Alert>
                )}

                <Button
                  type="button"
                  className="h-14 w-full text-base"
                  disabled={isLoading}
                  onClick={handleViewerLogin}
                >
                  {isLoading ? 'Opening...' : 'Printfarm Dashboard'}
                </Button>
              </div>
            )}

            <Button
              type="button"
              variant="outline"
              className="h-14 w-full border-sky-200 bg-sky-100 text-base text-sky-800 hover:bg-sky-200 hover:text-sky-900 dark:border-sky-800 dark:bg-sky-900/80 dark:text-sky-100 dark:hover:bg-sky-900"
              disabled={!googleFormUrl}
              onClick={() => window.open(googleFormUrl, '_blank', 'noopener,noreferrer')}
            >
              <ClipboardList className="mr-2 size-5" />
              ฟอร์มขอพิมพ์งาน
            </Button>
          </div>
        </Card>
      </div>
    </div>
  );
}
