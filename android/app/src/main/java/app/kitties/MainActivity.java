package app.kitties;

import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.Intent;
import android.graphics.Insets;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.view.WindowInsets;
import android.webkit.JavascriptInterface;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebView;

import androidx.webkit.WebViewAssetLoader;
import androidx.webkit.WebViewClientCompat;

// The app is the desktop app's page, served from the APK's own files at a fixed https origin
// (the gateway allows exactly that origin, with the token), talking to a gateway on the network.
// Links to anywhere else open in the phone's browser.
public class MainActivity extends Activity {
    static final String ORIGIN_HOST = "appassets.androidplatform.net";
    static final String HOME = "https://" + ORIGIN_HOST + "/index.html";

    private WebView web;

    @Override
    protected void onCreate(Bundle saved) {
        super.onCreate(saved);
        web = new WebView(this);
        web.setBackgroundColor(0xFF0A0A0F);
        setContentView(web);
        fitSystemBars();

        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        s.setAllowFileAccess(false);
        s.setAllowContentAccess(false);
        // The page is https; a gateway on the home network is plain http.
        s.setMixedContentMode(WebSettings.MIXED_CONTENT_ALWAYS_ALLOW);

        final WebViewAssetLoader assets = new WebViewAssetLoader.Builder()
                .setDomain(ORIGIN_HOST)
                .addPathHandler("/", new WebViewAssetLoader.AssetsPathHandler(this))
                .build();
        web.setWebViewClient(new WebViewClientCompat() {
            @Override
            public WebResourceResponse shouldInterceptRequest(WebView view, WebResourceRequest req) {
                return assets.shouldInterceptRequest(req.getUrl());
            }

            @Override
            public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest req) {
                Uri u = req.getUrl();
                if (ORIGIN_HOST.equals(u.getHost())) {
                    return false;
                }
                openOutside(u.toString());
                return true;
            }
        });
        // Alerts and confirmations ("Remove this model?") need a chrome client to show at all.
        web.setWebChromeClient(new WebChromeClient());
        web.addJavascriptInterface(new Bridge(), "KittiesAndroid");

        if (saved != null) {
            web.restoreState(saved);
        } else {
            web.loadUrl(HOME);
        }
    }

    // Android 15 draws apps under the status and navigation bars: keep the page clear of them and
    // of the keyboard.
    private void fitSystemBars() {
        if (Build.VERSION.SDK_INT < 35) {
            return;
        }
        web.setOnApplyWindowInsetsListener((v, insets) -> {
            Insets bars = insets.getInsets(WindowInsets.Type.systemBars() | WindowInsets.Type.ime()
                    | WindowInsets.Type.displayCutout());
            v.setPadding(bars.left, bars.top, bars.right, bars.bottom);
            return WindowInsets.CONSUMED;
        });
    }

    void openOutside(String url) {
        if (!url.startsWith("https://") && !url.startsWith("http://")) {
            return;
        }
        try {
            startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(url)));
        } catch (ActivityNotFoundException e) {
            // No browser: nothing to open it with.
        }
    }

    // Bridge is what the page calls: window.KittiesAndroid.open(url).
    final class Bridge {
        @JavascriptInterface
        public void open(String url) {
            runOnUiThread(() -> openOutside(url));
        }
    }

    @Override
    protected void onSaveInstanceState(Bundle out) {
        super.onSaveInstanceState(out);
        web.saveState(out);
    }

    @Override
    public void onBackPressed() {
        if (web.canGoBack()) {
            web.goBack();
            return;
        }
        super.onBackPressed();
    }
}
