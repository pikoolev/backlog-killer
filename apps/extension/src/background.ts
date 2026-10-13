const API='https://api.webmaker.pw';
const OIDC='https://auth.webmaker.pw';
const REALM='backlog-killer';
const CLIENT='backlog-killer-extension';
// A new session key deliberately excludes tokens left by older extension releases.
const SESSION_KEY='authSessionV4';
const SESSION_SCHEMA=4;
const WINDOW_KEY='boundWebWindowV4';

type Session={schema:number;accessToken:string;refreshToken:string;expiresAt:number};
type ActiveTab={id:number;windowId:number;url:string;title:string};
const bytes=(size:number)=>{const value=new Uint8Array(size);crypto.getRandomValues(value);return value};
const b64=(value:ArrayBuffer|Uint8Array)=>btoa(String.fromCharCode(...(value instanceof Uint8Array?value:new Uint8Array(value)))).replaceAll('+','-').replaceAll('/','_').replaceAll('=','');
const isWebTab=(tab:chrome.tabs.Tab):tab is chrome.tabs.Tab&{id:number;url:string}=>typeof tab.id==='number'&&typeof tab.url==='string'&&/^https?:\/\//.test(tab.url);
const toActiveTab=(tab:chrome.tabs.Tab&{id:number;url:string}):ActiveTab=>({id:tab.id,windowId:tab.windowId,url:tab.url,title:tab.title??''});

// Each invocation invalidates pending work synchronously, before any storage or network await.
let authEpoch=0;
let mutation=Promise.resolve();
let refreshing:Promise<Session|null>|null=null;
let refreshEpoch=-1;
const requests=new Set<AbortController>();
function serialize(action:()=>Promise<void>){const pending=mutation.then(action);mutation=pending.catch(()=>{});return pending}
function cancelRequests(){for(const controller of requests)controller.abort();requests.clear()}
async function invalidate(){authEpoch++;cancelRequests();const stored=(await chrome.storage.session.get(SESSION_KEY))[SESSION_KEY] as Session|undefined;await serialize(()=>chrome.storage.session.remove(SESSION_KEY));return stored?.schema===SESSION_SCHEMA?stored.refreshToken:null}
function assertCurrent(epoch:number){if(epoch!==authEpoch)throw new Error('Авторизация отменена')}
async function login(){
 const epoch=++authEpoch;cancelRequests();await serialize(()=>chrome.storage.session.remove(SESSION_KEY));assertCurrent(epoch);
 const redirect=chrome.identity.getRedirectURL('oidc');const verifier=b64(bytes(48));const challenge=b64(await crypto.subtle.digest('SHA-256',new TextEncoder().encode(verifier)));const state=b64(bytes(24));assertCurrent(epoch);
 const auth=new URL(`${OIDC}/realms/${REALM}/protocol/openid-connect/auth`);auth.search=new URLSearchParams({client_id:CLIENT,redirect_uri:redirect,response_type:'code',scope:'openid email profile',prompt:'login',code_challenge:challenge,code_challenge_method:'S256',state}).toString();
 const callback=await chrome.identity.launchWebAuthFlow({url:auth.toString(),interactive:true});assertCurrent(epoch);if(!callback)throw new Error('Авторизация отменена');const result=new URL(callback);if(result.searchParams.get('state')!==state)throw new Error('Некорректный OIDC state');const code=result.searchParams.get('code');if(!code)throw new Error(result.searchParams.get('error_description')??'OIDC code отсутствует');
 const body=new URLSearchParams({grant_type:'authorization_code',client_id:CLIENT,redirect_uri:redirect,code,code_verifier:verifier});const response=await fetch(`${OIDC}/realms/${REALM}/protocol/openid-connect/token`,{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});assertCurrent(epoch);if(!response.ok)throw new Error('Не удалось получить сессию');const token=await response.json() as {access_token:string;refresh_token:string;expires_in:number};assertCurrent(epoch);
 const active:Session={schema:SESSION_SCHEMA,accessToken:token.access_token,refreshToken:token.refresh_token,expiresAt:Date.now()+token.expires_in*1000};await serialize(async()=>{if(epoch===authEpoch)await chrome.storage.session.set({[SESSION_KEY]:active})});assertCurrent(epoch);return active;
}
async function session(){
 const epoch=authEpoch;const stored=(await chrome.storage.session.get(SESSION_KEY))[SESSION_KEY] as Session|undefined;assertCurrent(epoch);if(!stored||stored.schema!==SESSION_SCHEMA)return null;if(stored.expiresAt>Date.now()+60000)return stored;
 if(refreshing&&refreshEpoch===epoch)return refreshing;
 const attempt=(async()=>{const body=new URLSearchParams({grant_type:'refresh_token',client_id:CLIENT,refresh_token:stored.refreshToken});let response:Response;try{response=await fetch(`${OIDC}/realms/${REALM}/protocol/openid-connect/token`,{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body})}catch(error){assertCurrent(epoch);throw error}assertCurrent(epoch);
  if(!response.ok){await serialize(async()=>{if(epoch===authEpoch)await chrome.storage.session.remove(SESSION_KEY)});assertCurrent(epoch);return null}
  const token=await response.json() as {access_token:string;refresh_token?:string;expires_in:number};assertCurrent(epoch);const next:Session={schema:SESSION_SCHEMA,accessToken:token.access_token,refreshToken:token.refresh_token??stored.refreshToken,expiresAt:Date.now()+token.expires_in*1000};await serialize(async()=>{if(epoch===authEpoch)await chrome.storage.session.set({[SESSION_KEY]:next})});assertCurrent(epoch);return next})();
 refreshing=attempt;refreshEpoch=epoch;try{return await attempt}finally{if(refreshing===attempt){refreshing=null;refreshEpoch=-1}}
}
async function api(path:string,init?:RequestInit){const epoch=authEpoch;const active=await session();assertCurrent(epoch);if(!active)throw new Error('Требуется вход');const controller=new AbortController();requests.add(controller);try{const response=await fetch(API+path,{...init,signal:controller.signal,headers:{...init?.headers,Authorization:`Bearer ${active.accessToken}`}});const text=await response.text();assertCurrent(epoch);if(!response.ok){let detail;try{detail=JSON.parse(text).message}catch{}throw new Error(detail??`HTTP ${response.status}`)}return text?JSON.parse(text):null}finally{requests.delete(controller)}}
async function activeWebTab(windowId?:number){
 const bound=(await chrome.storage.session.get(WINDOW_KEY))[WINDOW_KEY] as number|undefined;
 const target=windowId??bound;
 if(typeof target==='number'){try{const tabs=await chrome.tabs.query({active:true,windowId:target});const tab=tabs[0];return tab&&isWebTab(tab)?toActiveTab(tab):null}catch{if(windowId!==undefined)throw new Error('Окно сайта недоступно')}}
 const tabs=await chrome.tabs.query({active:true,lastFocusedWindow:true});const tab=tabs[0];return tab&&isWebTab(tab)?toActiveTab(tab):null;
}
async function clearCanvases(){const tabs=await chrome.tabs.query({});await Promise.allSettled(tabs.filter(isWebTab).map(tab=>chrome.tabs.sendMessage(tab.id,{type:'render-tickets',tickets:[]})))}
async function logout(){const refreshToken=await invalidate();void clearCanvases().catch(()=>{});if(refreshToken){const body=new URLSearchParams({client_id:CLIENT,token:refreshToken,token_type_hint:'refresh_token'});void fetch(`${OIDC}/realms/${REALM}/protocol/openid-connect/revoke`,{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body}).catch(()=>{})}}
const notifyTabChanged=()=>{void chrome.runtime.sendMessage({type:'active-tab-changed'}).catch(()=>{})};
chrome.runtime.onInstalled.addListener(()=>{void chrome.storage.session.remove([SESSION_KEY,'authSessionV3','authGenerationV3','boundWebTabV3']);void chrome.storage.local.remove(['authSession','authSessionV2','authSessionV3'])});
chrome.tabs.onActivated.addListener(info=>{void chrome.storage.session.set({[WINDOW_KEY]:info.windowId}).then(notifyTabChanged)});
chrome.tabs.onUpdated.addListener((_id,change,tab)=>{if((change.status==='complete'||change.url)&&tab.active)notifyTabChanged()});
chrome.tabs.onRemoved.addListener(()=>notifyTabChanged());
chrome.runtime.onMessage.addListener((message,_sender,send)=>{if(!['login','session','active-tab','logout','api'].includes(message?.type))return false;void(async()=>{try{if(message.type==='login')send({ok:true,session:await login()});else if(message.type==='session')send({ok:true,session:await session()});else if(message.type==='active-tab')send({ok:true,data:await activeWebTab(message.windowId)});else if(message.type==='logout'){await logout();send({ok:true})}else if(message.type==='api')send({ok:true,data:await api(message.path,message.init)})}catch(error){send({ok:false,error:error instanceof Error?error.message:String(error)})}})();return true});
chrome.action.onClicked.addListener(tab=>{void chrome.storage.session.set({[WINDOW_KEY]:tab.windowId});void chrome.sidePanel.open({windowId:tab.windowId}).catch(()=>{})});
