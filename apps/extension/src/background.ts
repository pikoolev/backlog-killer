const API='https://api.webmaker.pw';
const OIDC='https://auth.webmaker.pw';
const REALM='backlog-killer';
const CLIENT='backlog-killer-extension';
const SESSION_KEY='authSessionV3';
const GENERATION_KEY='authGenerationV3';
const TAB_KEY='boundWebTabV3';
const SESSION_SCHEMA=3;

type Session={schema:number;accessToken:string;refreshToken:string;expiresAt:number};
type BoundTab={id:number;windowId:number;url:string;title:string};
const bytes=(size:number)=>{const value=new Uint8Array(size);crypto.getRandomValues(value);return value};
const b64=(value:ArrayBuffer|Uint8Array)=>btoa(String.fromCharCode(...new Uint8Array(value instanceof Uint8Array?value.buffer:value))).replaceAll('+','-').replaceAll('/','_').replaceAll('=','');
const isWebTab=(tab:chrome.tabs.Tab):tab is chrome.tabs.Tab&{id:number;url:string}=>typeof tab.id==='number'&&typeof tab.url==='string'&&/^https?:\/\//.test(tab.url);
const toBoundTab=(tab:chrome.tabs.Tab&{id:number;url:string}):BoundTab=>({id:tab.id,windowId:tab.windowId,url:tab.url,title:tab.title??''});
async function generation(){return Number((await chrome.storage.session.get(GENERATION_KEY))[GENERATION_KEY]??0)}
async function invalidateLogin(){const next=await generation()+1;await chrome.storage.session.set({[GENERATION_KEY]:next});await chrome.storage.session.remove(SESSION_KEY);return next}
async function bindTab(tab:chrome.tabs.Tab){if(isWebTab(tab))await chrome.storage.session.set({[TAB_KEY]:toBoundTab(tab)})}
async function login(){
 const loginGeneration=await invalidateLogin();
 const redirect=chrome.identity.getRedirectURL('oidc');const verifier=b64(bytes(48));const challenge=b64(await crypto.subtle.digest('SHA-256',new TextEncoder().encode(verifier)));const state=b64(bytes(24));
 const auth=new URL(`${OIDC}/realms/${REALM}/protocol/openid-connect/auth`);auth.search=new URLSearchParams({client_id:CLIENT,redirect_uri:redirect,response_type:'code',scope:'openid email profile',prompt:'login',code_challenge:challenge,code_challenge_method:'S256',state}).toString();
 const callback=await chrome.identity.launchWebAuthFlow({url:auth.toString(),interactive:true});if(!callback)throw new Error('Авторизация отменена');if(await generation()!==loginGeneration)throw new Error('Авторизация отменена');const result=new URL(callback);if(result.searchParams.get('state')!==state)throw new Error('Некорректный OIDC state');const code=result.searchParams.get('code');if(!code)throw new Error(result.searchParams.get('error_description')??'OIDC code отсутствует');
 const body=new URLSearchParams({grant_type:'authorization_code',client_id:CLIENT,redirect_uri:redirect,code,code_verifier:verifier});const response=await fetch(`${OIDC}/realms/${REALM}/protocol/openid-connect/token`,{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});if(!response.ok)throw new Error('Не удалось получить сессию');if(await generation()!==loginGeneration)throw new Error('Авторизация отменена');const token=await response.json() as {access_token:string;refresh_token:string;expires_in:number};const active:Session={schema:SESSION_SCHEMA,accessToken:token.access_token,refreshToken:token.refresh_token,expiresAt:Date.now()+token.expires_in*1000};await chrome.storage.session.set({[SESSION_KEY]:active});return active;
}
async function session(){const stored=(await chrome.storage.session.get(SESSION_KEY))[SESSION_KEY] as Session|undefined;if(!stored||stored.schema!==SESSION_SCHEMA)return null;if(stored.expiresAt>Date.now()+60000)return stored;const currentGeneration=await generation();const body=new URLSearchParams({grant_type:'refresh_token',client_id:CLIENT,refresh_token:stored.refreshToken});const response=await fetch(`${OIDC}/realms/${REALM}/protocol/openid-connect/token`,{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});if(!response.ok||await generation()!==currentGeneration){await chrome.storage.session.remove(SESSION_KEY);return null}const token=await response.json() as {access_token:string;refresh_token?:string;expires_in:number};const next:Session={schema:SESSION_SCHEMA,accessToken:token.access_token,refreshToken:token.refresh_token??stored.refreshToken,expiresAt:Date.now()+token.expires_in*1000};await chrome.storage.session.set({[SESSION_KEY]:next});return next}
async function activeWebTab(){const stored=(await chrome.storage.session.get(TAB_KEY))[TAB_KEY] as BoundTab|undefined;if(stored){try{const tab=await chrome.tabs.get(stored.id);if(isWebTab(tab)){const value=toBoundTab(tab);await chrome.storage.session.set({[TAB_KEY]:value});return value}}catch{await chrome.storage.session.remove(TAB_KEY)}}const windows=await chrome.windows.getAll({populate:true,windowTypes:['normal']});for(const window of windows){const tab=window.tabs?.find(item=>item.active&&isWebTab(item));if(tab&&isWebTab(tab)){const value=toBoundTab(tab);await chrome.storage.session.set({[TAB_KEY]:value});return value}}return null}
async function clearCanvases(){const tabs=await chrome.tabs.query({});await Promise.allSettled(tabs.filter(isWebTab).map(tab=>chrome.tabs.sendMessage(tab.id,{type:'render-tickets',tickets:[]})))}
async function logout(){await invalidateLogin();void clearCanvases()}
const notifyTabChanged=()=>{void chrome.runtime.sendMessage({type:'active-tab-changed'}).catch(()=>{})};
chrome.runtime.onInstalled.addListener(()=>{void chrome.storage.session.clear()});
chrome.action.onClicked.addListener(tab=>{void bindTab(tab).then(()=>{if(tab.windowId)return chrome.sidePanel.open({windowId:tab.windowId})})});
chrome.tabs.onActivated.addListener(async info=>{try{const tab=await chrome.tabs.get(info.tabId);await bindTab(tab);notifyTabChanged()}catch{}});
chrome.tabs.onUpdated.addListener((_tabId,change,tab)=>{if((change.status==='complete'||change.url)&&tab.active)void bindTab(tab).then(notifyTabChanged)});
chrome.runtime.onMessage.addListener((message,_sender,send)=>{void(async()=>{try{if(message.type==='login')send({ok:true,session:await login()});else if(message.type==='session')send({ok:true,session:await session()});else if(message.type==='active-tab')send({ok:true,data:await activeWebTab()});else if(message.type==='logout'){await logout();send({ok:true})}else if(message.type==='api'){const active=await session();if(!active)throw new Error('Требуется вход');const response=await fetch(API+message.path,{...message.init,headers:{...message.init?.headers,Authorization:`Bearer ${active.accessToken}`}});const text=await response.text();if(!response.ok)throw new Error(JSON.parse(text||'{}').message??`HTTP ${response.status}`);send({ok:true,data:text?JSON.parse(text):null})}}catch(error){send({ok:false,error:error instanceof Error?error.message:String(error)})}})();return true});
