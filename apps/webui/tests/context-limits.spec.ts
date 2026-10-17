import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
const origin=`http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT??18882}`;
const base=`http://127.0.0.1:${process.env.MOLR_TEST_PORT??5193}`;
async function post(page:Page,path:string,data:object){
 const token=(await page.context().cookies()).find(c=>c.name===`molr_dev_csrf_p${new URL(base).port}`)!.value;
 const r=await page.request.post(path,{headers:{Origin:base,'X-CSRF-Token':token},data:{Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:0},...data}});
 expect(r.status(),await r.text()).toBe(200);return r.json();
}
test('unset context stays blank when saving, manual override can be cleared',async({page},info)=>{
 await page.goto('/');await page.getByRole('textbox',{name:'Username',exact:true}).fill('owner');await page.getByLabel('Password',{exact:true}).fill('webui-e2e-password-2026!');await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.getByRole('navigation',{name:'Molr'}).waitFor();
 const name=`Context ${info.project.name}-${Date.now()}`;
 const resource=await post(page,'/api/resources',{Name:name,Kind:'model',Provider:'openai-compatible',Binding:{Endpoint:origin,AllowedOrigins:[origin],Models:[{Name:name,ContextLimit:0,SupportsTools:true,Pricing:{Currency:'USD',InputMicrosPerMillion:0,OutputMicrosPerMillion:0,CachedInputMicrosPerMillion:0}}]}});
 await page.goto('/#/resources');await page.reload();const provider=page.locator('.resource-row').filter({has:page.getByRole('heading',{name,exact:true})});await provider.locator('h2 button').click();const row=page.locator('.provider-models-page').first();await expect(row).toContainText('262,144 tokens · System fallback');
 const open=async()=>{await row.getByRole('button',{name,exact:true}).click();return page.getByRole('dialog',{name:'Configure model',exact:true});};
 let dialog=await open();let field=dialog.getByRole('spinbutton',{name:'Manual context limit (tokens, optional)',exact:true});await expect(field).toHaveValue('');await expect(field).toHaveAttribute('placeholder','262144');await expect(dialog.getByRole('spinbutton',{name:'Manual output limit (tokens, optional)',exact:true})).toHaveValue('');await expect(dialog.getByRole('spinbutton',{name:'Manual output limit (tokens, optional)',exact:true})).toHaveAttribute('placeholder','32768');await dialog.getByRole('button',{name:'Save',exact:true}).click();await expect(dialog).not.toBeVisible();
 const catalog=async()=>{const all=await(await page.request.get('/api/models')).json();return all.find((m:any)=>m.ResourceID===resource.ID);};expect((await catalog()).ContextLimit).toBe(0);
 dialog=await open();field=dialog.getByRole('spinbutton',{name:'Manual context limit (tokens, optional)',exact:true});await field.fill('65536');await dialog.getByRole('button',{name:'Save',exact:true}).click();await expect(row).toContainText('65,536 tokens · Manual');
 dialog=await open();await dialog.getByRole('spinbutton',{name:'Manual context limit (tokens, optional)',exact:true}).fill('');await dialog.getByRole('button',{name:'Save',exact:true}).click();await expect(row).toContainText('262,144 tokens · System fallback');expect((await catalog()).ContextLimit).toBe(0);
});


test('model output limit inherits provider, saves manual override and clears it', async ({page}, info) => {
 await page.goto('/');
 await page.getByRole('textbox',{name:'Username',exact:true}).fill('owner');
 await page.getByLabel('Password',{exact:true}).fill('webui-e2e-password-2026!');
 await page.getByRole('button',{name:'Sign in',exact:true}).click();
 await page.getByRole('navigation',{name:'Molr'}).waitFor();
 const name=`Output ${info.project.name}-${Date.now()}`;
 const resource=await post(page,'/api/resources',{Name:name,Kind:'model',Provider:'openai-compatible',Binding:{Endpoint:origin,AllowedOrigins:[origin],Models:[{Name:name,OutputLimit:0,ProviderOutputLimit:8192,SupportsTools:true}]}});
 await page.goto('/#/resources');await page.reload();
 const provider=page.locator('.resource-row').filter({has:page.getByRole('heading',{name,exact:true})});
 await provider.locator('h2 button').click();
 const row=page.locator('.provider-models-page').first();
 await expect(row).toContainText('Maximum output8,192 tokens · Provider');
 const open=async()=>{await row.getByRole('button',{name,exact:true}).click();return page.getByRole('dialog',{name:'Configure model',exact:true});};
 let dialog=await open();
 let field=dialog.getByRole('spinbutton',{name:'Manual output limit (tokens, optional)',exact:true});
 await expect(field).toHaveValue('');await expect(field).toHaveAttribute('placeholder','8192');
 await field.fill('65536');await dialog.getByRole('button',{name:'Save',exact:true}).click();
 await expect(row).toContainText('Maximum output65,536 tokens · Manual');
 await page.reload();await provider.locator('h2 button').click();
 dialog=await open();field=dialog.getByRole('spinbutton',{name:'Manual output limit (tokens, optional)',exact:true});
 await expect(field).toHaveValue('65536');await field.fill('');
 await dialog.getByRole('button',{name:'Save',exact:true}).click();
 await expect(row).toContainText('Maximum output8,192 tokens · Provider');
 const all=await(await page.request.get('/api/models')).json();
 const model=all.find((m:any)=>m.ResourceID===resource.ID);
 expect(model.OutputLimit).toBe(0);expect(model.EffectiveOutputLimit).toBe(8192);
});
