# Git CI Monitor

Ce programme Go surveille automatiquement un repository Git pour détecter les nouveaux commits sur toutes les branches et lance les tests e2e de manière séquentielle.

## Fonctionnalités

- **Surveillance automatique** : Vérifie périodiquement les nouveaux commits sur toutes les branches distantes
- **Limitation quotidienne** : Maximum d'un run de tests par branche par jour
- **Tests séquentiels** : Un seul test à la fois pour éviter les conflits de ressources
- **Configuration flexible** : Options personnalisables via fichier JSON
- **Persistance d'état** : Sauvegarde l'état entre les redémarrages

## Installation

```bash
cd git-ci-monitor
go mod tidy
go build -o git-ci-monitor
```

## Configuration

Créez un fichier `config.json` :

```json
{
  "repo_path": "/path/to/fink-broker",
  "check_interval": "5m",
  "test_script": "./e2e/fink-ci.sh",
  "max_runs_per_day": 1,
  "options": "-c -i ztf"
}
```

### Paramètres

- `repo_path` : Chemin vers le repository fink-broker
- `check_interval` : Intervalle de vérification (format Go duration: "5m", "1h", etc.)
- `test_script` : Script de test à exécuter (généralement `./e2e/fink-ci.sh`)
- `max_runs_per_day` : Nombre maximum de runs par branche par jour
- `options` : Options à passer au script fink-ci.sh

### Options pour fink-ci.sh

D'après le script `e2e/fink-ci.sh`, les options disponibles sont :

- `-c` : Nettoie le cluster si les tests réussissent
- `-s` : Utilise les algorithmes scientifiques pendant les tests
- `-i <survey>` : Spécifie le survey d'entrée (par défaut: ztf)
- `-b <branch>` : Nom de la branche (automatiquement ajouté par le monitor)
- `-m` : Active le monitoring

Exemples d'options :
- `"-c -i ztf"` : Cleanup + survey ZTF
- `"-c -s -i ztf"` : Cleanup + science + survey ZTF
- `"-c -s -m -i ztf"` : Cleanup + science + monitoring + survey ZTF

## Utilisation

### Démarrage

```bash
# Avec fichier de configuration
./git-ci-monitor config.json

# Avec configuration par défaut
./git-ci-monitor
```

### Variables d'environnement requises

Le script fink-ci.sh nécessite :

```bash
export TOKEN="your-github-token"
export USER="your-username"
```

### Fonctionnement

1. **Surveillance** : Le programme vérifie périodiquement les branches distantes
2. **Détection** : Quand un nouveau commit est détecté, il est ajouté à la queue
3. **Limitation** : Vérifie si la limite quotidienne n'est pas atteinte
4. **Exécution** : Lance le script de test avec les bonnes options
5. **Séquentiel** : Un seul test à la fois, les autres attendent dans la queue

### Fichiers générés

- `.git-ci-monitor-state.json` : État persistant (derniers commits, compteurs quotidiens)

## Logs

Le programme affiche des logs détaillés :

```
2024/01/15 10:00:00 Starting Git CI Monitor...
2024/01/15 10:00:00 Repository: /path/to/fink-broker
2024/01/15 10:00:00 Check interval: 5m0s
2024/01/15 10:00:00 Max runs per day: 1
2024/01/15 10:00:00 Options: -c -i ztf
2024/01/15 10:00:00 Starting test runner...
2024/01/15 10:00:00 Checking for updates...
2024/01/15 10:05:00 New commit detected on branch feature-xyz: abcd1234
2024/01/15 10:05:00 Queued test job for branch feature-xyz
2024/01/15 10:05:00 Starting tests for branch feature-xyz, commit abcd1234
```

## Arrêt gracieux

Le programme peut être arrêté proprement avec Ctrl+C. Il sauvegarde automatiquement son état avant de se fermer.

## Architecture

- **Monitor** : Structure principale qui gère la surveillance
- **TestJob** : Job de test dans la queue
- **BranchState** : État d'une branche (dernier commit, runs du jour)
- **Config** : Configuration chargée depuis JSON

Le programme utilise des goroutines pour :
- La surveillance périodique des branches
- L'exécution séquentielle des tests