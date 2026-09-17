# Stack technique du Homelab Portal

## Forme applicative

Le portail est une application unique dans un dépôt unique. `web/` contient Astro, Tailwind CSS et le JavaScript natif ; `cmd/` et `internal/` contiennent le BFF Go. Les dépendances front-end et Go sont verrouillées, mises à jour par pull request testée, et ne doivent jamais utiliser de versions flottantes.

Astro produit des assets statiques pendant le build. Ils sont embarqués dans le binaire Go, qui sert l'interface, l'authentification OIDC, le catalogue et les diagnostics. Node est donc présent uniquement dans l'étape de build, jamais dans l'image d'exécution. L'UI n'utilise ni CDN, ni framework client, ni bibliothèque de composants : Tailwind produit le CSS local et JavaScript natif couvre recherche, filtres et états d'interface.

Le BFF repose sur `net/http`, `client-go`, `go-oidc` et `slog`. Il n'utilise ni framework HTTP, ni base de données, ni Redis. Il est le seul serveur du catalogue et ne fournit pas d'API publique distincte.

## Identité et catalogue

Keycloak fournit un client OIDC confidentiel `homelab-portal`, avec Authorization Code + PKCE, scopes `openid profile groups`, claim JSON `groups`, groupe administrateur exact `portal-admin`, callback `/auth/callback` et déconnexion `/auth/logout`. Le navigateur ne reçoit jamais de token. Le BFF crée une session chiffrée et signée dans un cookie `Secure`, `HttpOnly`, `SameSite=Lax`.

Une session dure au plus 4 heures et expire après 30 minutes d'inactivité. Elle ne contient pas de refresh token. La clé de session admet une clé courante et une clé précédente lors d'une rotation ; une déconnexion détruit la session locale. Une indisponibilité temporaire de Keycloak n'annule pas une session locale avant son expiration, mais interdit les nouvelles connexions.

Le catalogue dérive uniquement des Ingress `networking.k8s.io/v1` de classe `tailscale` publiés par métadonnées GitOps valides et possédant exactement un hostname LoadBalancer utilisable. La cible est strictement `https://<hostname>`. Les IP, URL construites, cibles non HTTP et sondages sortants sont exclus. `available` signifie que cette publication est valide et actuelle, jamais que l'application répond à HTTP.

Les applications cibles gardent entièrement leur propre authentification et autorisation. Le portail ne gère que la visibilité d'une carte. Les cartes `admin` et les diagnostics sont réservés à `portal-admin`.

## Kubernetes et sécurité

La cible est Kubernetes `v1.36.4+k3s1`; l'image est publiée pour `linux/amd64` et `linux/arm64`. Le conteneur Go est une image distroless non-root, sans shell ni gestionnaire de paquets, avec racine en lecture seule, `allowPrivilegeEscalation: false`, capacités Linux supprimées et profil seccomp `RuntimeDefault`.

Le portail démarre avec une réplique, sans PVC. Ses ressources sont `25m` CPU et `48Mi` en requests, `100m` CPU et `128Mi` en limits, avec 20 secondes d'arrêt gracieux. Un `ClusterRole` cross-namespace n'accorde que `get`, `list` et `watch` sur les Ingress. Aucun Secret, Pod, Service, Deployment ou droit d'écriture n'est accordé.

Une `NetworkPolicy` deny-by-default autorise seulement l'entrée depuis le proxy Tailscale et le scrape Prometheus, ainsi que la sortie vers kube-dns, l'API Kubernetes et Keycloak. Son application effective dépend du CNI du cluster et doit être vérifiée au déploiement.

Le premier list Kubernetes a un délai de 10 secondes. Le watch utilise un backoff avec jitter de 1 à 30 secondes et reliste après `410 Gone`; son snapshot est atomique. Le dernier catalogue valide reste affiché, est signalé dégradé après 2 minutes, puis expiré après 15 minutes : `/readyz` échoue alors, mais les derniers liens restent affichés avec leur état obsolète.

## Exposition, observabilité et livraison

L'Ingress Tailscale est l'unique exposition du portail. `/healthz`, `/readyz` et `/metrics` sont servis seulement via le Service interne et ne figurent jamais sur l'Ingress. Prometheus peut les scraper depuis le cluster. Des règles GitOps alertent si le list initial échoue ou le cache expire, et avertissent si le watch se reconnecte plus de deux minutes ou si des annotations restent invalides.

Les logs JSON stdout utilisent `INFO` en production, un identifiant de requête, l'événement et son résultat. Ils excluent adresse IP, utilisateur, groupe, cookies, tokens, codes OIDC, en-têtes `Authorization` et URL sensibles. Les routes d'authentification sont limitées à 10 tentatives par IP sur cinq minutes; un dépassement retourne `429` avec un blocage visible de dix minutes.

L'application est livrée par Kustomize et Argo CD. Les manifests de l'application sont dans son dépôt; le dépôt homelab porte le namespace, l'Application Argo au wave 23, et l'Ingress centralisé au wave 21. Les images sont identifiées par tag immuable et digest. Chaque release produit SBOM, scan de vulnérabilités et signature avant une promotion humaine. Un retour arrière est un retour au commit Git précédent.

La barrière de release couvre tests unitaires, faux client Kubernetes, contrats OIDC, RBAC négatif, intégration Keycloak, e2e in-cluster Tailscale, synchronisation VSO, santé Argo CD et limite mémoire du proxy Tailscale.
