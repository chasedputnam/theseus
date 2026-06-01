#!/bin/bash
set -e

INSTALL_DIR=/opt/theseus
SERVICE_USER=theseus

echo "Installing Theseus..."

# Create user
id -u $SERVICE_USER &>/dev/null || useradd -r -s /bin/false $SERVICE_USER

# Create directories
mkdir -p $INSTALL_DIR/data $INSTALL_DIR/static

# Copy binary and static files
cp theseus $INSTALL_DIR/
cp -r static/ $INSTALL_DIR/static/
chown -R $SERVICE_USER:$SERVICE_USER $INSTALL_DIR

# Install service
cp theseus.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable theseus
systemctl start theseus

echo "Theseus installed. Check logs: journalctl -u theseus -f"
