function o(r){const t=new Uint8Array(32);crypto.getRandomValues(t);const e=Array.from(t,n=>n.toString(16).padStart(2,"0")).join("");return`${r}${e}`}export{o as g};
