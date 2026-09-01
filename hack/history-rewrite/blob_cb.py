import re
if b'PRIVATE KEY-----' in blob.data:
    blob.data = re.sub(
        rb'-----BEGIN ([A-Z ]*)PRIVATE KEY-----.*?-----END \1PRIVATE KEY-----',
        b'<REDACTED: demo SSH private key removed before open-sourcing>',
        blob.data, flags=re.S)
