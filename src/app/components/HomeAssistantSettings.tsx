import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { ArrowLeftRight, Loader2, Plug, RefreshCw, Trash2 } from 'lucide-react';
import { Card } from './ui/card';
import { Switch } from './ui/switch';
import { Input } from './ui/input';
import { Textarea } from './ui/textarea';
import { Label } from './ui/label';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select';
import {
  createHaRule,
  deleteHaRule,
  fetchHaRules,
  fetchHomeAssistantConfig,
  fetchHomeAssistantDevices,
  saveHomeAssistantConfig,
  setHaRuleEnabled,
  testHomeAssistantConnection,
  type HaDevices,
  type HaRule,
  type HaRuleDirection,
} from '../lib/homeAssistantApi';
import { fetchPrinters } from '../lib/printersApi';
import type { Printer, PrinterStatus } from '../types';

interface HomeAssistantSettingsProps {
  // Only admins may change anything; others see a read-only view.
  disabled?: boolean;
}

// Common Home Assistant services offered as datalist suggestions for the action
// field; the admin can still type any `domain.service`.
const COMMON_SERVICES = [
  'switch.turn_on',
  'switch.turn_off',
  'switch.toggle',
  'light.turn_on',
  'light.turn_off',
  'light.toggle',
  'fan.turn_on',
  'fan.turn_off',
  'climate.set_temperature',
  'notify.notify',
  'script.turn_on',
];

// Domains hidden from the "Devices & entities" card — these are noise for picking
// a controllable device. They remain available as automation-builder suggestions
// (a printer-status sensor is a common trigger entity).
const HIDDEN_CARD_DOMAINS = new Set([
  'automation',
  'sensor',
  'binary_sensor',
  'sun',
  'zone',
  'update',
  'person',
  'number',
  'device_tracker',
  'camera',
]);

const PRINTER_STATUSES: PrinterStatus[] = ['printing', 'idle', 'paused', 'error', 'offline'];
const PRINTER_COMMANDS = ['pause', 'resume', 'cancel'] as const;

// Radix Select can't use an empty-string item value, so an optional ("none")
// selection is represented by this sentinel and mapped back to '' on change.
const NONE_VALUE = '__none__';

// Themed entity picker (replaces the native <datalist>, which renders an
// unthemed browser dropdown). Populated from the loaded device list.
function EntitySelect({
  value,
  onChange,
  entities,
  disabled,
  allowNone = false,
  emptyLabel = 'Load devices first',
}: {
  value: string;
  onChange: (value: string) => void;
  entities: { entityId: string; friendlyName: string }[];
  disabled?: boolean;
  allowNone?: boolean;
  emptyLabel?: string;
}) {
  return (
    <Select
      value={value || (allowNone ? NONE_VALUE : '')}
      onValueChange={(v) => onChange(v === NONE_VALUE ? '' : v)}
      disabled={disabled}
    >
      <SelectTrigger>
        <SelectValue placeholder={entities.length ? 'Select an entity' : emptyLabel} />
      </SelectTrigger>
      <SelectContent>
        {allowNone && <SelectItem value={NONE_VALUE}>(none)</SelectItem>}
        {entities.map((entity) => (
          <SelectItem key={entity.entityId} value={entity.entityId}>
            {entity.friendlyName}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

// Settings → Home Assistant. Self-contained: loads its own config, device list,
// printers, and automation rules. The long-lived token is write-only — the server
// returns only whether one is stored, so a blank field on save keeps the token.
export function HomeAssistantSettings({ disabled = false }: HomeAssistantSettingsProps) {
  const [baseUrl, setBaseUrl] = useState('');
  const [token, setToken] = useState('');
  const [hasToken, setHasToken] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);

  const [devices, setDevices] = useState<HaDevices | null>(null);
  const [loadingDevices, setLoadingDevices] = useState(false);

  const [printers, setPrinters] = useState<Printer[]>([]);
  const [rules, setRules] = useState<HaRule[]>([]);
  const [loadingRules, setLoadingRules] = useState(false);

  // Automation rule builder.
  const [direction, setDirection] = useState<HaRuleDirection>('printer_to_ha');
  const [name, setName] = useState('');
  const [printerId, setPrinterId] = useState('');
  const [triggerEntity, setTriggerEntity] = useState('');
  const [triggerState, setTriggerState] = useState('');
  const [printerCommand, setPrinterCommand] = useState<(typeof PRINTER_COMMANDS)[number]>('pause');
  const [printerStatus, setPrinterStatus] = useState<PrinterStatus>('idle');
  const [actionService, setActionService] = useState('');
  const [actionEntity, setActionEntity] = useState('');
  const [actionData, setActionData] = useState('');
  const [creating, setCreating] = useState(false);

  const [togglingId, setTogglingId] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);

  useEffect(() => {
    // The HA config/device/rule endpoints are admin-only, so don't even attempt
    // the reads for a non-admin (avoids spurious error toasts).
    if (disabled) return;
    let cancelled = false;
    fetchHomeAssistantConfig()
      .then((config) => {
        if (cancelled) return;
        setBaseUrl(config.baseUrl);
        setHasToken(config.hasToken);
        setEnabled(config.enabled);
      })
      .catch(() => {
        toast.error('Unable to load Home Assistant settings.');
      });
    fetchPrinters()
      .then((list) => {
        if (!cancelled) setPrinters(list);
      })
      .catch(() => {
        /* printers are optional context for the rule builder */
      });
    loadRules();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [disabled]);

  const configured = baseUrl.trim().length > 0 && (hasToken || token.trim().length > 0);
  const printerName = (id: string) => printers.find((p) => p.id === id)?.name ?? id;

  const handleSave = async (event: React.FormEvent) => {
    event.preventDefault();
    if (disabled) {
      toast.error('Only admins can change Home Assistant settings.');
      return;
    }
    const trimmedUrl = baseUrl.trim();
    if (enabled && (!trimmedUrl || (!hasToken && !token.trim()))) {
      toast.error('A Home Assistant URL and access token are required to enable the integration.');
      return;
    }
    setSaving(true);
    try {
      const saved = await saveHomeAssistantConfig({ baseUrl: trimmedUrl, token: token.trim(), enabled });
      setBaseUrl(saved.baseUrl);
      setHasToken(saved.hasToken);
      setEnabled(saved.enabled);
      setToken('');
      toast.success('Home Assistant settings saved.');
    } catch (error) {
      toast.error('Unable to save Home Assistant settings.', {
        description: error instanceof Error ? error.message : undefined,
      });
    } finally {
      setSaving(false);
    }
  };

  const handleTest = async () => {
    setTesting(true);
    try {
      const result = await testHomeAssistantConnection();
      if (result.ok) {
        toast.success(result.message ?? 'Connected to Home Assistant.');
      } else {
        toast.error('Connection failed.', { description: result.error });
      }
    } catch (error) {
      toast.error('Connection failed.', {
        description: error instanceof Error ? error.message : undefined,
      });
    } finally {
      setTesting(false);
    }
  };

  const loadDevices = async () => {
    setLoadingDevices(true);
    try {
      setDevices(await fetchHomeAssistantDevices());
    } catch (error) {
      toast.error('Unable to load Home Assistant devices.', {
        description: error instanceof Error ? error.message : undefined,
      });
    } finally {
      setLoadingDevices(false);
    }
  };

  const loadRules = async () => {
    setLoadingRules(true);
    try {
      setRules(await fetchHaRules());
    } catch (error) {
      toast.error('Unable to load automation rules.', {
        description: error instanceof Error ? error.message : undefined,
      });
    } finally {
      setLoadingRules(false);
    }
  };

  const handleCreateRule = async (event: React.FormEvent) => {
    event.preventDefault();
    if (disabled) {
      toast.error('Only admins can create automations.');
      return;
    }
    if (!name.trim() || !printerId) {
      toast.error('A name and a printer are required.');
      return;
    }
    if (direction === 'ha_to_printer' && (!triggerEntity.trim() || !triggerState.trim())) {
      toast.error('A trigger entity and state are required.');
      return;
    }
    if (direction === 'printer_to_ha' && (!actionService.trim() || !actionEntity.trim())) {
      toast.error('An action service and target entity are required.');
      return;
    }
    setCreating(true);
    try {
      await createHaRule({
        name: name.trim(),
        direction,
        enabled: true,
        printerId,
        triggerEntity: triggerEntity.trim(),
        triggerState: triggerState.trim(),
        printerCommand,
        printerStatus,
        actionService: actionService.trim(),
        actionEntity: actionEntity.trim(),
        actionData: actionData.trim(),
      });
      toast.success('Automation created.', { description: name.trim() });
      setName('');
      setTriggerEntity('');
      setTriggerState('');
      setActionService('');
      setActionEntity('');
      setActionData('');
      await loadRules();
    } catch (error) {
      toast.error('Unable to create automation.', {
        description: error instanceof Error ? error.message : undefined,
      });
    } finally {
      setCreating(false);
    }
  };

  const handleToggleRule = async (rule: HaRule, next: boolean) => {
    setTogglingId(rule.id);
    try {
      await setHaRuleEnabled(rule.id, next);
      setRules((current) => current.map((r) => (r.id === rule.id ? { ...r, enabled: next } : r)));
    } catch (error) {
      toast.error('Unable to update automation.', {
        description: error instanceof Error ? error.message : undefined,
      });
    } finally {
      setTogglingId(null);
    }
  };

  const handleDeleteRule = async (rule: HaRule) => {
    if (disabled) {
      toast.error('Only admins can delete automations.');
      return;
    }
    setDeletingId(rule.id);
    try {
      await deleteHaRule(rule.id);
      toast.success('Automation deleted.');
      setRules((current) => current.filter((r) => r.id !== rule.id));
    } catch (error) {
      toast.error('Unable to delete automation.', {
        description: error instanceof Error ? error.message : undefined,
      });
    } finally {
      setDeletingId(null);
    }
  };

  const describeRule = (rule: HaRule) =>
    rule.direction === 'ha_to_printer'
      ? `When ${rule.triggerEntity} → “${rule.triggerState}”, ${rule.printerCommand} ${printerName(rule.printerId)}`
      : `When ${printerName(rule.printerId)} → “${rule.printerStatus}”, call ${rule.actionService}${
          rule.actionEntity ? ` on ${rule.actionEntity}` : ''
        }`;

  const entities = devices?.entities ?? [];

  // The "Do" target entity must be controllable by the chosen service, so once a
  // service is picked the entity picker is narrowed to that service's domain
  // (e.g. `light.turn_on` → only `light.*` entities).
  const serviceDomain = actionService.includes('.') ? actionService.split('.')[0] : '';
  const actionEntities = serviceDomain
    ? entities.filter((entity) => entity.domain === serviceDomain)
    : entities;

  // Changing the service may strip the previously-selected entity of its domain
  // match, so drop a now-incompatible selection.
  const handleActionServiceChange = (service: string) => {
    setActionService(service);
    const domain = service.includes('.') ? service.split('.')[0] : '';
    if (actionEntity && domain && !entities.some((e) => e.entityId === actionEntity && e.domain === domain)) {
      setActionEntity('');
    }
  };

  // Card view drops noisy domains (automations, sensors); the builder datalists
  // still use the full `entities` list above.
  const cardGroups = Object.entries(devices?.groups ?? {}).filter(
    ([domain, items]) => !HIDDEN_CARD_DOMAINS.has(domain) && items.length > 0,
  );
  const cardEntityCount = cardGroups.reduce((sum, [, items]) => sum + items.length, 0);

  return (
    <div className="space-y-6">
      {/* Connection */}
      <Card className="p-6 dark:bg-gray-900 dark:border-gray-800">
        <form onSubmit={handleSave} className="space-y-6">
          <div>
            <h2 className="text-xl font-semibold dark:text-white">Home Assistant connection</h2>
            <p className="mt-1 text-sm text-gray-600 dark:text-gray-400">
              Connect to a Home Assistant instance with its base URL and a{' '}
              <span className="font-medium">long-lived access token</span> (create one in HA under
              your profile → Security → Long-lived access tokens). The token is stored encrypted and
              never returned to the browser.
            </p>
          </div>

          <div className="flex items-center justify-between rounded-lg border border-gray-200 p-4 dark:border-gray-700">
            <div>
              <Label htmlFor="ha-enabled" className="text-base">
                Enable Home Assistant
              </Label>
              <p className="text-sm text-gray-500 dark:text-gray-400">
                Required for fetching devices and for the automation rules to run.
              </p>
            </div>
            <Switch id="ha-enabled" checked={enabled} onCheckedChange={setEnabled} disabled={disabled} />
          </div>

          <div className="space-y-2">
            <Label htmlFor="ha-base-url">Home Assistant URL</Label>
            <Input
              id="ha-base-url"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder="http://homeassistant.local:8123"
              disabled={disabled}
              spellCheck={false}
              autoComplete="off"
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="ha-token">Long-lived access token</Label>
            <Input
              id="ha-token"
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={hasToken ? '•••••••• (leave blank to keep)' : 'Paste the access token'}
              disabled={disabled}
              spellCheck={false}
              autoComplete="off"
            />
            <p className="text-xs text-gray-500 dark:text-gray-400">
              {hasToken
                ? 'A token is stored. Leave blank to keep it, or enter a new one to replace it.'
                : 'No token stored yet.'}
            </p>
          </div>

          <div className="flex flex-wrap gap-2">
            <Button type="submit" disabled={saving || disabled}>
              {saving ? 'Saving...' : 'Save connection'}
            </Button>
            <Button type="button" variant="outline" onClick={handleTest} disabled={testing || !configured}>
              {testing ? <Loader2 className="size-4 animate-spin" /> : <Plug className="size-4" />}
              Test connection
            </Button>
          </div>
        </form>
      </Card>

      {/* Device list */}
      <Card className="p-6 dark:bg-gray-900 dark:border-gray-800">
        <div className="mb-4 flex items-center justify-between gap-3">
          <div>
            <h2 className="text-xl font-semibold dark:text-white">Devices &amp; entities</h2>
            <p className="mt-1 text-sm text-gray-600 dark:text-gray-400">
              Controllable devices reported by Home Assistant, grouped by domain (non-controllable
              entities like sensors, automations, cameras, people, and trackers are hidden). Use
              these entity IDs when building an automation below.
            </p>
          </div>
          <Button type="button" variant="outline" onClick={loadDevices} disabled={loadingDevices || !configured}>
            {loadingDevices ? <Loader2 className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
            {devices ? 'Refresh' : 'Load devices'}
          </Button>
        </div>

        {devices && cardEntityCount === 0 && (
          <p className="text-sm text-gray-500 dark:text-gray-400">No controllable devices found.</p>
        )}

        {devices && cardEntityCount > 0 && (
          <div className="space-y-4">
            {cardGroups
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([domain, items]) => (
                <div key={domain}>
                  <div className="mb-2 flex items-center gap-2">
                    <h3 className="font-medium capitalize dark:text-gray-200">{domain}</h3>
                    <Badge variant="secondary">{items.length}</Badge>
                  </div>
                  <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                    {items.map((entity) => (
                      <div
                        key={entity.entityId}
                        className="rounded-md border border-gray-200 p-2 text-sm dark:border-gray-700"
                      >
                        <div className="truncate font-medium dark:text-gray-100" title={entity.friendlyName}>
                          {entity.friendlyName}
                        </div>
                        <div className="truncate font-mono text-xs text-gray-500 dark:text-gray-400" title={entity.entityId}>
                          {entity.entityId}
                        </div>
                        <div className="text-xs text-gray-400">{entity.state}</div>
                      </div>
                    ))}
                  </div>
                </div>
              ))}
          </div>
        )}
      </Card>

      {/* Automation builder */}
      <Card className="p-6 dark:bg-gray-900 dark:border-gray-800">
        <div className="mb-4 flex items-center gap-2">
          <ArrowLeftRight className="size-5 text-gray-500" />
          <h2 className="text-xl font-semibold dark:text-white">Create an automation</h2>
        </div>
        <p className="mb-4 text-sm text-gray-600 dark:text-gray-400">
          Bridges the print farm and Home Assistant. These rules are evaluated by the server (not
          stored inside Home Assistant), so they can read printer status and send printer commands.
        </p>

        <form onSubmit={handleCreateRule} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label>Direction</Label>
              <Select value={direction} onValueChange={(v) => setDirection(v as HaRuleDirection)} disabled={disabled}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="printer_to_ha">Printer event → Home Assistant action</SelectItem>
                  <SelectItem value="ha_to_printer">Home Assistant event → printer command</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="ha-rule-name">Name</Label>
              <Input
                id="ha-rule-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Lights off when print done"
                disabled={disabled}
              />
            </div>
          </div>

          {/* WHEN */}
          <div className="rounded-lg border border-gray-200 p-4 dark:border-gray-700">
            <p className="mb-3 text-xs font-semibold uppercase tracking-wide text-gray-500">When…</p>
            {direction === 'printer_to_ha' ? (
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label>Printer</Label>
                  <Select value={printerId} onValueChange={setPrinterId} disabled={disabled}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select a printer" />
                    </SelectTrigger>
                    <SelectContent>
                      {printers.map((printer) => (
                        <SelectItem key={printer.id} value={printer.id}>
                          {printer.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label>Status becomes</Label>
                  <Select value={printerStatus} onValueChange={(v) => setPrinterStatus(v as PrinterStatus)} disabled={disabled}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {PRINTER_STATUSES.map((status) => (
                        <SelectItem key={status} value={status}>
                          {status}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            ) : (
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label>Home Assistant entity</Label>
                  <EntitySelect
                    value={triggerEntity}
                    onChange={setTriggerEntity}
                    entities={entities}
                    disabled={disabled}
                  />
                  {entities.length === 0 && (
                    <p className="text-xs text-gray-400">Load devices above to choose an entity.</p>
                  )}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="ha-trigger-state">State becomes</Label>
                  <Input
                    id="ha-trigger-state"
                    value={triggerState}
                    onChange={(e) => setTriggerState(e.target.value)}
                    placeholder="off"
                    disabled={disabled}
                    spellCheck={false}
                    autoComplete="off"
                  />
                </div>
              </div>
            )}
          </div>

          {/* DO */}
          <div className="rounded-lg border border-gray-200 p-4 dark:border-gray-700">
            <p className="mb-3 text-xs font-semibold uppercase tracking-wide text-gray-500">Do…</p>
            {direction === 'printer_to_ha' ? (
              <div className="space-y-4">
                <div className="grid gap-4 sm:grid-cols-2">
                  <div className="space-y-2">
                    <Label>Call service</Label>
                    <Select value={actionService} onValueChange={handleActionServiceChange} disabled={disabled}>
                      <SelectTrigger>
                        <SelectValue placeholder="Select a service" />
                      </SelectTrigger>
                      <SelectContent>
                        {COMMON_SERVICES.map((service) => (
                          <SelectItem key={service} value={service}>
                            {service}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-2">
                    <Label>Target entity</Label>
                    <EntitySelect
                      value={actionEntity}
                      onChange={setActionEntity}
                      entities={actionEntities}
                      disabled={disabled || !serviceDomain}
                      emptyLabel={
                        !serviceDomain
                          ? 'Select a service first'
                          : entities.length
                            ? `No ${serviceDomain} entities`
                            : 'Load devices first'
                      }
                    />
                  </div>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="ha-action-data">Extra service data (optional JSON)</Label>
                  <Textarea
                    id="ha-action-data"
                    value={actionData}
                    onChange={(e) => setActionData(e.target.value)}
                    placeholder='{ "brightness": 200 }'
                    rows={2}
                    disabled={disabled}
                    spellCheck={false}
                  />
                </div>
              </div>
            ) : (
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label>Printer</Label>
                  <Select value={printerId} onValueChange={setPrinterId} disabled={disabled}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select a printer" />
                    </SelectTrigger>
                    <SelectContent>
                      {printers.map((printer) => (
                        <SelectItem key={printer.id} value={printer.id}>
                          {printer.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label>Command</Label>
                  <Select
                    value={printerCommand}
                    onValueChange={(v) => setPrinterCommand(v as (typeof PRINTER_COMMANDS)[number])}
                    disabled={disabled}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {PRINTER_COMMANDS.map((command) => (
                        <SelectItem key={command} value={command}>
                          {command}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            )}
          </div>

          <Button type="submit" disabled={creating || disabled}>
            {creating ? 'Creating...' : 'Create automation'}
          </Button>
        </form>
      </Card>

      {/* Existing rules */}
      <Card className="p-6 dark:bg-gray-900 dark:border-gray-800">
        <div className="mb-4 flex items-center justify-between gap-3">
          <h2 className="text-xl font-semibold dark:text-white">Automation rules</h2>
          <Button type="button" variant="outline" onClick={loadRules} disabled={loadingRules}>
            {loadingRules ? <Loader2 className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
            Refresh
          </Button>
        </div>

        {rules.length === 0 ? (
          <p className="text-sm text-gray-500 dark:text-gray-400">No automation rules yet.</p>
        ) : (
          <ul className="divide-y divide-gray-200 dark:divide-gray-700">
            {rules.map((rule) => (
              <li key={rule.id} className="flex items-center justify-between gap-3 py-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium dark:text-gray-100">{rule.name}</span>
                    <Badge variant="secondary">
                      {rule.direction === 'ha_to_printer' ? 'HA → printer' : 'printer → HA'}
                    </Badge>
                  </div>
                  <div className="truncate text-xs text-gray-500 dark:text-gray-400">{describeRule(rule)}</div>
                </div>
                <div className="flex shrink-0 items-center gap-3">
                  <Switch
                    checked={rule.enabled}
                    onCheckedChange={(next) => handleToggleRule(rule, next)}
                    disabled={disabled || togglingId === rule.id}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => handleDeleteRule(rule)}
                    disabled={disabled || deletingId === rule.id}
                    title="Delete rule"
                  >
                    {deletingId === rule.id ? (
                      <Loader2 className="size-4 animate-spin" />
                    ) : (
                      <Trash2 className="size-4" />
                    )}
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}
