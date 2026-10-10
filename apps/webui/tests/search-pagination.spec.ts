import {test,expect} from '@playwright/test';

test('search updates query and page together across typing, filters and clearing',async({page})=>{
 await page.goto('/');
 await page.getByLabel('Username',{exact:true}).fill('owner');
 await page.getByLabel('Password',{exact:true}).fill('webui-e2e-password-2026!');
 await page.getByRole('button',{name:'Sign in',exact:true}).click();
 await expect(page.getByRole('navigation',{name:'Molr',exact:true})).toBeVisible();
 const requests:{query:string;cursor:string;sources:string[]}[]=[];
 // Fixed logical pages isolate query timing from ranking and catalog contents.
 await page.route('**/api/search?*',async route=>{
  const params=new URL(route.request().url()).searchParams;
  const query=params.get('q')??'',cursor=params.get('cursor')??'',sources=params.getAll('source');requests.push({query,cursor,sources});
  const source=sources[0]??'assistants',title=`${query} ${cursor?'second':'first'} page`;
  await route.fulfill({json:{Counts:{[source]:51},Hits:{Items:[{Source:source,Object:{Kind:source==='messages'?'message':'assistant',ID:'synthetic-result'},AssistantID:'synthetic-result',Title:title,Snippet:title,Score:1}],NextCursor:cursor?'':`${query}-second`}}});
 });
 await expect(page.getByRole('complementary',{name:'Assistant list',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Search',exact:true}).first().click();
 const panel=page.getByRole('dialog',{name:'Search',exact:true}),input=panel.getByRole('searchbox');
 const result=async(query:string,which='first')=>expect(panel.getByText(`${query} ${which} page`,{exact:true}).first()).toBeVisible();
 const next=async(query:string)=>{await panel.getByRole('button',{name:'Next page',exact:true}).click();await result(query,'second');};
 await input.fill('older');await result('older');await next('older');
 expect(requests).toEqual([{query:'older',cursor:'',sources:[]},{query:'older',cursor:'older-second',sources:[]}]);requests.length=0;
 await test.step('new query starts at its first page without reloading old results',async()=>{
  await input.fill('newer');await result('newer');
  expect(requests).toEqual([{query:'newer',cursor:'',sources:[]}]);
 });
 await next('newer');requests.length=0;
 await test.step('surrounding whitespace preserves the current page',async()=>{
  await input.fill(' newer ');await page.waitForTimeout(350);await result('newer','second');expect(requests).toEqual([]);
 });
 await test.step('rapid typing requests only the settled query',async()=>{
  await input.fill('rapid-one');await input.fill('rapid-two');await input.fill('settled');await result('settled');
  expect(requests).toEqual([{query:'settled',cursor:'',sources:[]}]);
 });
 await next('settled');requests.length=0;
 await test.step('source changes reset the page for the current query',async()=>{
  await panel.getByRole('combobox',{name:'Filter search types',exact:true}).click();
  await page.getByRole('option',{name:'Messages',exact:true}).click();await page.keyboard.press('Escape');await result('settled');
  expect(requests).toEqual([{query:'settled',cursor:'',sources:['messages']}]);
 });
 await next('settled');requests.length=0;
 await test.step('clearing text and filters does not fetch the old first page',async()=>{
  await input.fill('');await expect(panel.locator('.workspace-search__shortcuts')).toBeVisible();await page.waitForTimeout(350);expect(requests).toEqual([]);
 });
 await test.step('closing during debounce cancels the pending query',async()=>{
  await input.fill('closed-query');
  // Escape in a native search input clears its value first. Close from the dialog heading.
  await panel.getByRole('heading',{name:'Search',exact:true}).focus();await page.keyboard.press('Escape');await expect(panel).not.toBeVisible();await page.waitForTimeout(350);expect(requests).toEqual([]);
  await page.getByRole('button',{name:'Search',exact:true}).first().click();await expect(input).toHaveValue('');await expect(panel.locator('.workspace-search__shortcuts')).toBeVisible();
 });
});
