# Installation de home-ci

## 1. Installation rapide avec go install

```bash
# Installation directe des binaires
go install github.com/k8s-school/home-ci/cmd/home-ci@latest
go install github.com/k8s-school/home-ci/cmd/home-ci-e2e@latest
go install github.com/k8s-school/home-ci/cmd/home-ci-diag@latest

# Vérifier l'installation
home-ci --help
home-ci-diag --help
```

## 2. Installation depuis les sources

```bash
# Cloner le projet
git clone https://github.com/k8s-school/home-ci.git
cd home-ci

# Construire tous les binaires
make build

# Installation système
sudo cp home-ci /usr/local/bin/
sudo cp home-ci-e2e /usr/local/bin/
sudo cp home-ci-diag /usr/local/bin/

# Vérifier l'installation
home-ci --help
home-ci-diag --help
```

## 3. Configuration de base

```bash
# Créer le répertoire de configuration
mkdir -p ~/.config/home-ci

# Créer un fichier de configuration minimal
cat > ~/.config/home-ci/config.yaml << EOF
repo_path: "/path/to/your/repo"
check_interval: 5m
test_script: "scripts/test.sh"
test_timeout: 10m
max_commit_age: 24h
fetch_remote: true
max_concurrent_runs: 2

cleanup:
  after_e2e: true
  script: "scripts/cleanup.sh"

github_actions_dispatch:
  enabled: false
EOF
```

## 4. Utilisation

```bash
# Lancer home-ci avec une configuration
home-ci --config ~/.config/home-ci/config.yaml --verbose 2

# Diagnostics
home-ci-diag --config ~/.config/home-ci/config.yaml --check-timeline
home-ci-diag --config ~/.config/home-ci/config.yaml --check-concurrency

# Tests e2e
home-ci-e2e --type=success --verbose 2
```

## 5. Installation via Makefile

Le projet inclut des cibles Makefile pour simplifier le développement :

```bash
# Construire et tester
make build
make test

# Tests spécifiques
make test-success
make test-quick
make test-normal

# Nettoyage
make clean
```

## 6. Service systemd (optionnel)

Pour un déploiement en production, créer un service systemd :

```bash
# Créer le fichier service
sudo tee /etc/systemd/system/home-ci.service << EOF
[Unit]
Description=Home-CI Continuous Integration
After=network.target

[Service]
Type=simple
User=ci
Group=ci
ExecStart=/usr/local/bin/home-ci --config /home/ci/.config/home-ci/config.yaml --verbose 2
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# Activer et démarrer le service
sudo systemctl enable home-ci
sudo systemctl start home-ci
```

## 7. Surveillance et diagnostics

```bash
# Vérifier les logs
journalctl -u home-ci -f

# Diagnostics de timeline
home-ci-diag --config ~/.config/home-ci/config.yaml --check-timeline

# Diagnostics de concurrence
home-ci-diag --config ~/.config/home-ci/config.yaml --check-concurrency
```