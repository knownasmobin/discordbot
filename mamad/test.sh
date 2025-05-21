#!/bin/bash

PROXY_FILE="youtube.txt"
TARGET_URL="https://www.youtube.com"
TIMEOUT=10

while IFS= read -r proxy; do
    echo "Testing proxy: $proxy"
    
    # Run curl with proxy and check status
    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --proxy "https://$proxy" --max-time $TIMEOUT "$TARGET_URL")
    
    if [ "$HTTP_STATUS" -eq 200 ]; then
        echo "[OK] Proxy $proxy works!"
    else
        echo "[FAIL] Proxy $proxy returned HTTP $HTTP_STATUS"
    fi

done < "$PROXY_FILE"
