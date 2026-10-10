import {BacklogKiller} from '@backlog-killer/sdk';
class ProductCard extends HTMLElement{constructor(){super();const root=this.attachShadow({mode:'open'});root.innerHTML='<style>:host{display:block;padding:24px;margin:20px;border:1px solid #ccc}</style><article data-backlog-id="product-total"><h2>Order total</h2><strong>$42.00</strong></article>'}}
customElements.define('product-card',ProductCard);
const products=document.querySelector('#products');
if(products)products.innerHTML='<product-card></product-card><product-card data-backlog-private></product-card>';
BacklogKiller.init({projectKey:'demo-project',apiBase:'http://127.0.0.1:18080',enableScreenshot:true,onError:error=>console.error('Backlog Killer:',error.message)});
