export const API=import.meta.env.VITE_API_URL??'http://api.localhost:18080';

export function message(value:unknown){return value instanceof Error?value.message:String(value)}

export async function api<T>(path:string,token:string,init?:RequestInit):Promise<T>{const headers=new Headers(init?.headers);headers.set('Authorization',`Bearer ${token}`);if(init?.body&&!headers.has('Content-Type')&&!(init.body instanceof FormData))headers.set('Content-Type','application/json');const response=await fetch(`${API}${path}`,{...init,headers});if(!response.ok){const problem=await response.json().catch(()=>({message:`HTTP ${response.status}`})) as {message?:string};throw new Error(problem.message??`HTTP ${response.status}`)}if(response.status===204)return undefined as T;return response.json() as Promise<T>}

export async function openAttachment(project:string,id:string,token:string){const response=await fetch(`${API}/v1/projects/${project}/attachments/${id}`,{headers:{Authorization:`Bearer ${token}`}});if(!response.ok)throw new Error(`HTTP ${response.status}`);const url=URL.createObjectURL(await response.blob());const opened=open(url,'_blank','noopener');if(!opened){URL.revokeObjectURL(url);throw new Error('Браузер заблокировал открытие вложения')}setTimeout(()=>URL.revokeObjectURL(url),60000)}

export function siteTicketURL(origin:string,path:string,id:string){const url=new URL(path,origin);url.searchParams.set('backlog-ticket',id);return url.toString()}
