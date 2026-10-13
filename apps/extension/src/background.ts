const API='https://api.webmaker.pw';
const OIDC='https://auth.webmaker.pw';
const REALM='backlog-killer';
const CLIENT='backlog-killer-extension';
const SESSION_SCHEMA=2;

type Session={schema:number;accessToken:string;refreshToken:string;expiresAt:number};
const bytes=(size:number)=>{const value=new Uint8Array(size);crypto.getRandomValues(value);return value};
const b64=(value:ArrayBuffer|Uint8Array)=>btoa(String.fromCharCode(...new Uint8Array(value instanceof Uint8Array?value.buffer:value))).replaceAll('+','-').replaceAll('/','_').replaceAll('=','');
async function clearSession(){await chrome.storage.session.clear()}
async function login(){
 await clearSession();
 const redirect=chrome.identity.getRedirectURL('oidc');const verifier=b64(bytes(48));const challenge=b64(await crypto.subtle.digest('SHA-256',new TextEncoder().encode(verifier)));const state=b64(bytes(24));
 const auth=new URL(`${OIDC}/realms/${REALM}/protocol/openid-connect/auth`);auth.search=new URLSearchParams({client_id:CLIENT,redirect_uri:redirect,response_type:'code',scope:'openid email profile',prompt:'login',code_challenge:challenge,code_challenge_method:'S256',state}).toString();
 const callback=await chrome.identity.launchWebAuthFlow({url:auth.toString(),interactive:true});if(!callback)throw new Error('Авторизация отменена');const result=new URL(callback);if(result.searchParams.get('state')!==state)throw new Error('Некорректный OIDC state');const code=result.searchParams.get('code');if(!code)throw new Error(result.searchParams.get('error_description')??'OIDC code отсутствует');
 const body=new URLSearchParams({grant_type:'authorization_code',client_id:CLIENT,redirect_uri:redirect,code,code_verifier:verifier});const response=await fetch(`${OIDC}/realms/${REALM}/protocol/openid-connect/token`,{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});if(!response.ok)throw new Error('Не удалось получить сессию');const token=await response.json() as {access_token:string;refresh_token:string;expires_in:number};const active:Session={schema:SESSION_SCHEMA,accessToken:token.access_token,refreshToken:token.refresh_token,expiresAt:Date.now()+token.expires_in*1000};await chrome.storage.session.set({session:active});return active;
}
async function session(){const stored=(await chrome.storage.session.get('session')).session as Session|undefined;if(!stored||stored.schema!==SESSION_SCHEMA){await clearSession();return null}if(stored.expiresAt>Date.now()+60000)return stored;const body=new URLSearchParams({grant_type:'refresh_token',client_id:CLIENT,refresh_token:stored.refreshToken});const response=await fetch(`${OIDC}/realms/${REALM}/protocol/openid-connect/token`,{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body});if(!response.ok){await clearSession();return null}const token=await response.json() as {access_token:string;refresh_token?:string;expires_in:number};const next:Session={schema:SESSION_SCHEMA,accessToken:token.access_token,refreshToken:token.refresh_token??stored.refreshToken,expiresAt:Date.now()+token.expires_in*1000};await chrome.storage.session.set({session:next});return next}
async function activeWebTab(){const window=await chrome.windows.getLastFocused({windowTypes:['normal']});if(!window.id)return null;const [tab]=await chrome.tabs.query({active:true,windowId:window.id});if(!tab?.id||!tab.url||!/^https?:\/\//.test(tab.url))return null;return{id:tab.id,windowId:tab.windowId,url:tab.url,title:tab.title??''}}
async function clearCanvases(){const tabs=await chrome.tabs.query({});await Promise.all(tabs.filter(tab=>tab.id&&tab.url&&/^https?:\/\//.test(tab.url)).map(tab=>chrome.tabs.sendMessage(tab.id!,{type:'render-tickets',tickets:[]}).catch(()=>{})))}
async function logout(){await clearSession();await clearCanvases()}
const notifyTabChanged=()=>{void chrome.runtime.sendMessage({type:'active-tab-changed'}).catch(()=>{})};
chrome.runtime.onInstalled.addListener(()=>{void clearSession()});
chrome.action.onClicked.addListener(tab=>{if(tab.windowId)void chrome.sidePanel.open({windowId:tab.windowId})});
chrome.tabs.onActivated.addListener(notifyTabChanged);
chrome.tabs.onUpdated.addListener((_tabId,change)=>{if(change.status==='complete'||change.url)notifyTabChanged()});
chrome.windows.onFocusChanged.addListener(windowId=>{if(windowId!==chrome.windows.WINDOW_ID_NONE)notifyTabChanged()});
chrome.runtime.onMessage.addListener((message,_sender,send)=>{void(async()=>{try{if(message.type==='login')send({ok:true,session:await login()});else if(message.type==='session')send({ok:true,session:await session()});else if(message.type==='active-tab')send({ok:true,data:await activeWebTab()});else if(message.type==='logout'){await logout();send({ok:true})}else if(message.type==='api'){const active=await session();if(!active)throw new Error('Требуется вход');const response=await fetch(API+message.path,{...message.init,headers:{...message.init?.headers,Authorization:`Bearer ${active.accessToken}`}});const text=await response.text();if(!response.ok)throw new Error(JSON.parse(text||'{}').message??`HTTP ${response.status}`);send({ok:true,data:text?JSON.parse(text):null})}}catch(error){send({ok:false,error:error instanceof Error?error.message:String(error)})}})();return true});
