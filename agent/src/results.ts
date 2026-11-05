/** Filter before data reaches model context or durable storage. This is best effort. */
export function sanitize(text:string,patterns:string[]=[]):string {
 let result=text
  .replace(/-----BEGIN [^-]*PRIVATE KEY-----[\s\S]*?-----END [^-]*PRIVATE KEY-----/g,'[REDACTED PRIVATE KEY]')
  .replace(/\b(Bearer|Basic)\s+[^\s"',;]+/gi,'$1 [REDACTED]')
  .replace(/((?:[\w.-]*(?:api[_-]?key|password|passwd|secret|token)[\w.-]*|authorization|cookie|set-cookie)["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;]+)/gi,'$1[REDACTED]')
  .replace(/(https?:\/\/)[^\s/@]+:[^\s/@]+@/gi,'$1[REDACTED]@');
 // Literal patterns avoid user-supplied regex execution/backtracking.
 for(const pattern of patterns) result=result.split(pattern).join('[REDACTED]');
 return result;
}
export interface Receipt {version:1;phase:'verified';container_id:string;changed?:boolean;recovered?:boolean;running?:boolean}
export function parseReceipt(text:string,id:string):Receipt {
 const result=JSON.parse(text);
 if(result.version!==1||result.container_id!==id||result.phase!=='verified') {
  throw new Error(`Lifecycle outcome not verified: ${sanitize(JSON.stringify(result)).slice(0,2000)}; observe before retry`);
 }
 return result;
}

/** Preserve JSON shape while filtering every string before persistence. */
export function sanitizeValue(value:unknown,patterns:string[]=[]):unknown {
 if(typeof value==='string') return sanitize(value,patterns);
 if(Array.isArray(value)) return value.map(item=>sanitizeValue(item,patterns));
 if(value&&typeof value==='object') return Object.fromEntries(Object.entries(value).map(([key,item])=>[
  key,/^(?:authorization|cookie|set-cookie|.*(?:api[_-]?key|password|passwd|secret|token).*)$/i.test(key)?'[REDACTED]':sanitizeValue(item,patterns),
 ]));
 return value;
}
