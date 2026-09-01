import re
# 1. AI-assistant attribution trailers (F-03).
lines = [l for l in message.split(b'\n') if b'anthropic.com' not in l.lower()]
out = b'\n'.join(lines)
# 2. Internal-system references in commit messages (F-07): a live shoot, the
#    cluster it belonged to, and links into the private apeirora/showroom repo.
out = re.sub(rb'\bsh-[0-9a-f]{8}\b', b'<shoot>', out)
out = out.replace(b'demo-sap-ironcore', b'<cluster>')
out = re.sub(rb'\bapeirora/showroom#\d+\b', b'an internal issue', out)
return re.sub(rb'\n{3,}', b'\n\n', out).rstrip() + b'\n'
