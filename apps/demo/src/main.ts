import {BacklogKiller} from '@backlog-killer/sdk';

class ProductCard extends HTMLElement {
  constructor() {
    super();
    const root=this.attachShadow({mode:'open'});
    const title=this.getAttribute('data-title')??'Product';
    const price=this.getAttribute('data-price')??'4 200 ₽';
    root.innerHTML=`<style>:host{display:block}.card{height:100%;background:#fff;border:1px solid #e2e8f0;border-radius:15px;overflow:hidden;box-shadow:0 1px 3px #0f172a12}.image{height:150px;background:linear-gradient(145deg,#f1f5f9,#e2e8f0);display:grid;place-items:center;font-size:42px}.body{padding:16px}.tag{font:700 10px system-ui;color:#6d28d9;background:#ede9fe;padding:4px 7px;border-radius:999px}.title{font:700 16px system-ui;color:#0f172a;margin:13px 0 6px}.price{font:800 20px system-ui;color:#0f172a}button{float:right;border:0;background:#6d28d9;color:#fff;border-radius:8px;padding:8px 11px;font-weight:700}</style><article class="card" data-backlog-id="product-${title.toLowerCase().replaceAll(' ','-')}"><div class="image">◈</div><div class="body"><span class="tag">В наличии</span><div class="title">${title}</div><strong class="price">${price}</strong><button type="button">Купить</button></div></article>`;
  }
}
customElements.define('product-card',ProductCard);
const products=document.querySelector('#products');
if(products)products.innerHTML='<product-card data-title="Аналитика Pro" data-price="4 200 ₽"></product-card><product-card data-title="Команда Plus" data-price="7 900 ₽"></product-card><product-card data-title="Агент MCP" data-price="12 500 ₽"></product-card>';
BacklogKiller.init({projectKey:import.meta.env.VITE_PROJECT_KEY??'demo-project',apiBase:import.meta.env.VITE_API_URL??'http://api.localhost:18080',enableScreenshot:true,onError:error=>console.error('Backlog Killer:',error.message)});
