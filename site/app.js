(function(){
var tabs=[].slice.call(document.querySelectorAll('[role=tab]'));
function pick(i){tabs.forEach(function(t,j){t.setAttribute('aria-selected',j===i);document.getElementById('p'+j).hidden=j!==i})}
tabs.forEach(function(t,i){t.addEventListener('click',function(){pick(i)});t.addEventListener('keydown',function(e){var n=e.key==='ArrowRight'?i+1:e.key==='ArrowLeft'?i-1:null;if(n!==null){n=(n+tabs.length)%tabs.length;pick(n);tabs[n].focus()}})});
var d=document.querySelector('[data-tab]');if(d)d.addEventListener('click',function(){pick(1)});
[].forEach.call(document.querySelectorAll('.copy'),function(b){b.addEventListener('click',function(){
var t=b.parentNode.querySelector('code').textContent,o=b.textContent;
if(navigator.clipboard)navigator.clipboard.writeText(t).then(function(){b.textContent=b.dataset.c;setTimeout(function(){b.textContent=o},1500)})})});
var lg=document.querySelector('.lang');if(lg)lg.addEventListener('click',function(){try{localStorage.setItem('kitties-lang',lg.getAttribute('hreflang'))}catch(e){}});
})();
