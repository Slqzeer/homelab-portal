export interface CatalogueItem {
  name: string;
  description: string;
  category: string;
  icon: string;
  targetURL: string;
}

export interface Diagnostic {
  namespace: string;
  ingress: string;
  rule: string;
  remediation: string;
}

export const devCatalogueItems: CatalogueItem[] = [
  {
    name: 'Grafana',
    description: 'Dashboards and visualizations',
    category: 'Monitoring',
    icon: 'grafana',
    targetURL: 'https://grafana.tail.example',
  },
  {
    name: 'Prometheus',
    description: 'Time-series metrics and alert exploration',
    category: 'Monitoring',
    icon: 'prometheus',
    targetURL: 'https://prometheus.tail.example',
  },
  {
    name: 'Keycloak',
    description: 'Single sign-on and identity administration',
    category: 'Identity',
    icon: 'keycloak',
    targetURL: 'https://keycloak.tail.example',
  },
  {
    name: 'Argo CD',
    description: 'Declarative delivery and application synchronization',
    category: 'Delivery',
    icon: 'argocd',
    targetURL: 'https://argocd.tail.example',
  },
  {
    name: 'A deliberately long service name used to check wrapping in development',
    description: 'Long fixture content makes narrow-screen and overflow regressions visible while editing the interface.',
    category: 'Development fixtures',
    icon: 'generic',
    targetURL: 'https://long-content.tail.example',
  },
];

export const devDiagnostics: Diagnostic[] = [
  {
    namespace: 'apps',
    ingress: 'legacy-dashboard',
    rule: 'invalid access annotation',
    remediation: 'Use public, authenticated, admin, or group:<name>.',
  },
  {
    namespace: 'monitoring',
    ingress: 'metrics',
    rule: 'missing description',
    remediation: 'Add portal.homelab.io/description.',
  },
];
